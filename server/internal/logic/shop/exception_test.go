package shop

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
)

// excApp 创建已关闭订单（status 9）并占用名额，用于异常单场景。
func excApp(t *testing.T, ctx context.Context, slotId, petId, userId string) string {
	t.Helper()
	appId := "app_e_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":     appId,
		"user_id":    userId,
		"pet_id":     petId,
		"slot_id":    slotId,
		"shop_id":    consts.ShopId,
		"item_id":    consts.ItemId,
		"price":      consts.PriceCent,
		"status":     consts.StatusTimeoutClosed,
		"created_at": time.Now(),
		"updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert closed app: %v", err)
	}
	return appId
}

func excRow(t *testing.T, appId, txn string, excType, status int) string {
	t.Helper()
	excId := "exc_t_" + guid.S()
	_, err := g.DB().Model("pay_exception").Data(g.Map{
		"exc_id":         excId,
		"shop_id":        consts.ShopId,
		"app_id":         appId,
		"transaction_id": txn,
		"amount":         consts.PriceCent,
		"exc_type":       excType,
		"status":         status,
		"created_at":     time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert exception: %v", err)
	}
	return excId
}

func TestExceptionList(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := excApp(t, ctx, slotId, "pet_2001", "user_1001")
	e1 := excRow(t, appId, "txn_e_"+guid.S(), consts.ExcTypeClosedAfterPay, consts.ExcStatusPending)
	e2 := excRow(t, appId, "txn_e_"+guid.S(), consts.ExcTypeDuplicate, consts.ExcStatusRefunded)
	defer func() {
		_, _ = g.DB().Model("pay_exception").WhereIn("exc_id", g.Slice{e1, e2}).Delete()
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("refund").Where("app_id", appId).Delete()
	}()

	// 全部
	_, total, err := ExceptionList(ctx, 0, 1, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total < 2 {
		t.Fatalf("total should >=2, got %d", total)
	}
	// 仅待处理
	_, totalP, _ := ExceptionList(ctx, consts.ExcStatusPending, 1, 20)
	if totalP < 1 {
		t.Fatal("pending list should not be empty")
	}
	// 店铺隔离
	_, totalO, _ := ExceptionList(staffCtx(consts.RoleManager, "shop_002"), 0, 1, 20)
	if totalO != 0 {
		t.Fatalf("cross-shop list should be 0, got %d", totalO)
	}
}

func TestProcessException(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := excApp(t, ctx, slotId, "pet_2001", "user_1001")
	excId := excRow(t, appId, "txn_p_"+guid.S(), consts.ExcTypeClosedAfterPay, consts.ExcStatusPending)
	defer func() {
		_, _ = g.DB().Model("pay_exception").Where("exc_id", excId).Delete()
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("refund").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("oper_log").Where("app_id", appId).Delete()
	}()

	res, err := ProcessException(ctx, excId)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if res.Status != consts.ExcStatusRefunded {
		t.Fatalf("status should be 2, got %d", res.Status)
	}

	// 全额退款单 type=3
	ref, _ := g.DB().Model("refund").Where("app_id", appId).Where("refund_type", consts.RefundTypeException).One()
	if ref.IsEmpty() {
		t.Fatal("exception refund row missing")
	}
	if ref["amount"].Int() != consts.PriceCent || ref["status"].Int() != consts.RefundStatusSuccess {
		t.Fatalf("unexpected refund: %v", ref)
	}

	// 预约状态保持关闭（不触碰 appointment.status）
	if v, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status"); v.Int() != consts.StatusTimeoutClosed {
		t.Fatalf("appointment status must stay 9, got %d", v.Int())
	}

	// oper_log
	n, _ := g.DB().Model("oper_log").Where("app_id", appId).Where("action", "EXC_REFUND").Count()
	if n != 1 {
		t.Fatal("EXC_REFUND oper_log missing")
	}

	// 已处理不可重复处理 → 50010
	if _, err := ProcessException(ctx, excId); err == nil || err.Error() != "50010" {
		t.Fatalf("repeat process should fail 50010, got %v", err)
	}
}

func TestExceptionRole(t *testing.T) {
	slotId := newSlotRow(t, context.Background(), consts.ShopId, time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour), consts.SlotActive)
	appId := excApp(t, context.Background(), slotId, "pet_2001", "user_1001")
	excId := excRow(t, appId, "txn_r_"+guid.S(), consts.ExcTypeClosedAfterPay, consts.ExcStatusPending)
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() {
		_, _ = g.DB().Model("pay_exception").Where("exc_id", excId).Delete()
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("refund").Where("app_id", appId).Delete()
	}()

	// 前台不可处理 → 40001
	if _, err := ProcessException(staffCtx(consts.RoleFront, consts.ShopId), excId); err == nil || err.Error() != "40001" {
		t.Fatalf("front process should fail 40001, got %v", err)
	}
	// 客服可处理（店长/客服）
	if _, err := ProcessException(staffCtx(consts.RoleSupport, consts.ShopId), excId); err != nil {
		t.Fatalf("support process should succeed: %v", err)
	}
}

func TestExceptionCrossShop(t *testing.T) {
	slotId := newSlotRow(t, context.Background(), consts.ShopId, time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour), consts.SlotActive)
	appId := excApp(t, context.Background(), slotId, "pet_2001", "user_1001")
	excId := excRow(t, appId, "txn_x_"+guid.S(), consts.ExcTypeClosedAfterPay, consts.ExcStatusPending)
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() { _, _ = g.DB().Model("pay_exception").Where("exc_id", excId).Delete() }()

	if _, err := ProcessException(staffCtx(consts.RoleManager, "shop_002"), excId); err == nil || err.Error() != "50001" {
		t.Fatalf("cross-shop process should fail 50001, got %v", err)
	}
}
