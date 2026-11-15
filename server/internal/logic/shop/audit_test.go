package shop

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
)

// auditApp 直插 status=3 待审核预约并占用名额。
func auditApp(t *testing.T, ctx context.Context, slotId, petId, userId string, applyAt time.Time) string {
	t.Helper()
	appId := "app_a_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":          appId,
		"user_id":         userId,
		"pet_id":          petId,
		"slot_id":         slotId,
		"shop_id":         consts.ShopId,
		"item_id":         consts.ItemId,
		"price":           consts.PriceCent,
		"status":          consts.StatusCancelAudit,
		"cancel_apply_at": applyAt,
		"cancel_reason":   "临时有事",
		"created_at":      applyAt.Add(-time.Hour),
		"updated_at":      applyAt,
	}).Insert()
	if err != nil {
		t.Fatalf("insert audit app: %v", err)
	}
	_, _ = quota.Occupy(ctx, slotId, petId, consts.MaxCapacity)
	return appId
}

func TestAuditList(t *testing.T) {
	ctx := staffCtx(consts.RoleSupport, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	a1 := auditApp(t, ctx, slotId, "pet_2001", "user_1001", time.Now().Add(-2*time.Hour))
	a2 := auditApp(t, ctx, slotId, "pet_2003", "user_1002", time.Now().Add(-1*time.Hour))
	defer func() {
		_, _ = g.DB().Model("appointment").WhereIn("app_id", g.Slice{a1, a2}).Delete()
		_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	}()

	list, total, err := AuditList(ctx, 1, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total < 2 {
		t.Fatalf("total should >=2, got %d", total)
	}
	// 申请时间正序
	idx := map[string]int{}
	for i, it := range list {
		idx[it.AppId] = i
	}
	if idx[a1] >= idx[a2] {
		t.Fatal("audit list should be apply time asc")
	}
	// 仅 status=3
	for _, it := range list {
		if it.Status != consts.StatusCancelAudit {
			t.Fatal("audit list should only contain status=3")
		}
	}

	// 店铺隔离
	_, total2, _ := AuditList(staffCtx(consts.RoleSupport, "shop_002"), 1, 20)
	if total2 != 0 {
		t.Fatalf("cross-shop audit list should be 0, got %d", total2)
	}
}

func TestAuditApprove(t *testing.T) {
	ctx := staffCtx(consts.RoleSupport, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := auditApp(t, ctx, slotId, "pet_2001", "user_1001", time.Now().Add(-time.Hour))
	defer func() {
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("refund").Where("app_id", appId).Delete()
		_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	}()

	res, err := AuditApprove(ctx, appId)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if res.Status != consts.StatusCancelled || res.Message != "已同意取消，退款已发起" {
		t.Fatalf("unexpected result: %+v", res)
	}

	// 审批字段
	if v, _ := g.DB().Model("appointment").Where("app_id", appId).Value("audit_result"); v.Int() != consts.AuditApprove {
		t.Fatal("audit_result should be 1")
	}
	// 退款单 type=1 全额
	ref, _ := g.DB().Model("refund").Where("app_id", appId).One()
	if ref.IsEmpty() {
		t.Fatal("refund row missing")
	}
	if ref["refund_type"].Int() != consts.RefundTypeCancel || ref["amount"].Int() != consts.PriceCent {
		t.Fatalf("unexpected refund: %v", ref)
	}
	// 名额释放
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity {
		t.Fatalf("quota should be released, remaining=%d", remaining)
	}
	// oper_log AUDIT
	n, _ := g.DB().Model("oper_log").Where("app_id", appId).Where("action", "AUDIT").Count()
	if n != 1 {
		t.Fatal("AUDIT oper_log missing")
	}

	// 重复审批 → 50007
	if _, err := AuditApprove(ctx, appId); err == nil || err.Error() != "50007" {
		t.Fatalf("repeat approve should fail 50007, got %v", err)
	}
}

func TestAuditReject(t *testing.T) {
	ctx := staffCtx(consts.RoleSupport, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := auditApp(t, ctx, slotId, "pet_2001", "user_1001", time.Now().Add(-time.Hour))
	defer func() {
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	}()

	res, err := AuditReject(ctx, appId, "证据不足")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if res.Status != consts.StatusConfirmed || res.Message != "已驳回，预约恢复有效，可正常到店" {
		t.Fatalf("unexpected result: %+v", res)
	}
	// 驳回后名额继续占用
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity-1 {
		t.Fatalf("quota should stay occupied, remaining=%d", remaining)
	}
	// 重复处理 → 50007
	if _, err := AuditReject(ctx, appId, "x"); err == nil || err.Error() != "50007" {
		t.Fatalf("repeat reject should fail 50007, got %v", err)
	}
}

func TestAuditRoleDenied(t *testing.T) {
	slotId := newSlotRow(t, context.Background(), consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	appId := auditApp(t, context.Background(), slotId, "pet_2001", "user_1001", time.Now().Add(-time.Hour))
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() {
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.Redis().Do(context.Background(), "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	}()

	// 前台不可审批
	if _, err := AuditApprove(staffCtx(consts.RoleFront, consts.ShopId), appId); err == nil || err.Error() != "40001" {
		t.Fatalf("front audit should fail 40001, got %v", err)
	}
	// 客服或店长可审批（店长兼管留痕）
	if _, err := AuditApprove(staffCtx(consts.RoleManager, consts.ShopId), appId); err != nil {
		t.Fatalf("manager audit should succeed: %v", err)
	}
	defer func() { _, _ = g.DB().Model("refund").Where("app_id", appId).Delete() }()
}

func TestRefundRetry(t *testing.T) {
	ctx := staffCtx(consts.RoleSupport, consts.ShopId)
	refundId := "ref_t_" + guid.S()
	appId := "app_r_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":     appId,
		"user_id":    "user_1001",
		"pet_id":     "pet_2001",
		"slot_id":    "slot_03",
		"shop_id":    consts.ShopId,
		"item_id":    consts.ItemId,
		"price":      consts.PriceCent,
		"status":     consts.StatusCancelled,
		"created_at": time.Now(),
		"updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.DB().Model("refund").Data(g.Map{
		"refund_id":   refundId,
		"app_id":      appId,
		"refund_type": consts.RefundTypeCancel,
		"amount":      consts.PriceCent,
		"status":      consts.RefundStatusFailed,
		"fail_msg":    "渠道超时",
		"retry_count": 1,
		"created_at":  time.Now(),
	}).Insert()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = g.DB().Model("refund").Where("refund_id", refundId).Delete()
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.Redis().Do(ctx, "DEL", "refund:retry:"+refundId)
	}()

	// 客服重试 → 成功
	res, err := RefundRetry(ctx, refundId)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if res.Status != consts.RefundStatusSuccess {
		t.Fatalf("refund should be success, got %d", res.Status)
	}
	ref, _ := g.DB().Model("refund").Where("refund_id", refundId).One()
	if ref["retry_count"].Int() != 2 {
		t.Fatalf("retry_count should be 2, got %d", ref["retry_count"].Int())
	}
	if ref["refunded_at"].IsNil() {
		t.Fatal("refunded_at should be set")
	}

	// 已成功再重试 → 50003
	if _, err := RefundRetry(ctx, refundId); err == nil || err.Error() != "50003" {
		t.Fatalf("retry after success should fail 50003, got %v", err)
	}

	// 店长退款重试 → 50008
	if _, err := RefundRetry(staffCtx(consts.RoleManager, consts.ShopId), refundId); err == nil || err.Error() != "50008" {
		t.Fatalf("manager retry should fail 50008, got %v", err)
	}
}
