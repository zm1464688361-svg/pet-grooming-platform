// Package shop B 端补充测试（退款单列表、重试状态机、时段守卫、完成幂等）。
package shop

import (
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
)

func refundRow(t *testing.T, appId string, status int64) string {
	t.Helper()
	refundId := "ref_x_" + guid.S()
	_, err := g.DB().Model("refund").Data(g.Map{
		"refund_id": refundId, "app_id": appId, "refund_type": consts.RefundTypeCancel,
		"amount": 9900, "status": status, "created_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert refund: %v", err)
	}
	return refundId
}

func TestAdminRefundList(t *testing.T) {
	ctx := staffCtx(consts.RoleSupport, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(96*time.Hour), time.Now().Add(97*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusCancelled)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	refundId := refundRow(t, appId, int64(consts.RefundStatusFailed))
	defer func() { _, _ = g.DB().Model("refund").Where("refund_id", refundId).Delete() }()

	// 全量列表
	list, total, err := AdminRefundList(ctx, 0, 1, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, r := range list {
		if r.RefundId == refundId {
			found = true
			if r.StatusName != "退款失败" || r.Amount != 9900 {
				t.Fatalf("row mismatch: %+v", r)
			}
		}
	}
	if !found || total < 1 {
		t.Fatalf("refund not listed, total=%d", total)
	}
	// 状态筛选
	list, _, err = AdminRefundList(ctx, consts.RefundStatusFailed, 1, 20)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	for _, r := range list {
		if r.Status != consts.RefundStatusFailed {
			t.Fatalf("filter leaked status=%d", r.Status)
		}
	}
	// 服务角色可查（列表为只读，重试才限客服）
	if _, _, err := AdminRefundList(staffCtx(consts.RoleService, consts.ShopId), 0, 1, 20); err != nil {
		t.Fatalf("svc list should be allowed, got %v", err)
	}
}

func TestRefundRetryStates(t *testing.T) {
	ctx := staffCtx(consts.RoleSupport, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(98*time.Hour), time.Now().Add(99*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusCancelled)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 退款中 → 重试成功
	r1 := refundRow(t, appId, int64(consts.RefundStatusPending))
	defer func() { _, _ = g.DB().Model("refund").Where("refund_id", r1).Delete() }()
	res, err := RefundRetry(ctx, r1)
	if err != nil || res.Status != consts.RefundStatusSuccess {
		t.Fatalf("retry pending: %v,%+v", err, res)
	}
	v, _ := g.DB().Model("refund").Where("refund_id", r1).Fields("status,retry_count").One()
	if v["retry_count"].Int() != 1 {
		t.Fatalf("retry_count should be 1, got %v", v["retry_count"])
	}
	// 已成功 → 50003
	if _, err := RefundRetry(ctx, r1); err == nil || err.Error() != "50003" {
		t.Fatalf("retry success should fail 50003, got %v", err)
	}
	// 不存在的退款单 → 报错（no rows）
	if _, err := RefundRetry(ctx, "ref_ghost"); err == nil {
		t.Fatal("ghost refund should fail")
	}
}

func TestFinishServiceIdempotentAndConflict(t *testing.T) {
	ctx := staffCtx(consts.RoleService, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(100*time.Hour), time.Now().Add(101*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	// 已完成订单重复登记完成 → 幂等
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusFinished)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	res, err := FinishService(ctx, appId)
	if err != nil || res.Message != "已完成登记" {
		t.Fatalf("idempotent finish: %v,%+v", err, res)
	}
	// 已取消订单 → 状态冲突
	appId2 := directApp(t, ctx, slotId, "pet_2003", "user_1002", consts.StatusCancelled)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId2).Delete() }()
	if _, err := FinishService(ctx, appId2); err == nil {
		t.Fatal("finish cancelled order should fail")
	}
}

func TestSlotGuardsWithOrder(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(102*time.Hour), time.Now().Add(103*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 有有效预约：改时间/暂停/删除 → 50002
	if err := UpdateSlotTime(ctx, slotId, time.Now().Add(104*time.Hour), time.Now().Add(105*time.Hour)); err == nil || err.Error() != "50002" {
		t.Fatalf("update with order should 50002, got %v", err)
	}
	if err := PauseSlot(ctx, slotId); err == nil || err.Error() != "50002" {
		t.Fatalf("pause with order should 50002, got %v", err)
	}
	if err := DeleteSlot(ctx, slotId); err == nil || err.Error() != "50002" {
		t.Fatalf("delete with order should 50002, got %v", err)
	}
}

func TestAuditApproveGhost(t *testing.T) {
	ctx := staffCtx(consts.RoleSupport, consts.ShopId)
	if _, err := AuditApprove(ctx, "app_ghost"); err == nil {
		t.Fatal("approve ghost order should fail")
	}
	if _, err := AuditReject(ctx, "app_ghost", "x"); err == nil {
		t.Fatal("reject ghost order should fail")
	}
}
