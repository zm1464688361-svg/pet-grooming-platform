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

// directApp 直插指定状态预约（测试用）。
func directApp(t *testing.T, ctx context.Context, slotId, petId, userId string, status int) string {
	t.Helper()
	appId := "app_v_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":     appId,
		"user_id":    userId,
		"pet_id":     petId,
		"slot_id":    slotId,
		"shop_id":    consts.ShopId,
		"item_id":    consts.ItemId,
		"price":      consts.PriceCent,
		"status":     status,
		"created_at": time.Now(),
		"updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert appointment: %v", err)
	}
	return appId
}

func TestVerifySuccess(t *testing.T) {
	ctx := staffCtx(consts.RoleFront, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	// 占名额
	_, _ = quota.Occupy(ctx, slotId, "pet_2001", consts.MaxCapacity)

	res, err := VerifyAppointment(ctx, appId)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Status != consts.StatusVerified {
		t.Fatalf("status should be 5, got %d", res.Status)
	}
	if res.Message != "核销成功，该预约已进入服务流程" {
		t.Fatalf("unexpected message: %s", res.Message)
	}

	// 名额保持占用（已核销永久占用）
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity-1 {
		t.Fatalf("quota should stay occupied, remaining=%d", remaining)
	}

	// oper_log VERIFY
	v, _ := g.DB().Model("oper_log").Where("app_id", appId).Where("action", "VERIFY").Count()
	if v != 1 {
		t.Fatal("VERIFY oper_log missing")
	}

	// 重复核销 → 50003
	if _, err := VerifyAppointment(ctx, appId); err == nil || err.Error() != "50003" {
		t.Fatalf("repeat verify should fail 50003, got %v", err)
	}
}

func TestVerifyAuditBlocking(t *testing.T) {
	ctx := staffCtx(consts.RoleFront, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	// 同一时段两只宠物：目标单已确认，另一只存在待审核取消申请(status 3)
	appTarget := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	appAudit := directApp(t, ctx, slotId, "pet_2003", "user_1002", consts.StatusCancelAudit)
	defer func() {
		_, _ = g.DB().Model("appointment").WhereIn("app_id", g.Slice{appTarget, appAudit}).Delete()
	}()

	if _, err := VerifyAppointment(ctx, appTarget); err == nil || err.Error() != "50004" {
		t.Fatalf("verify with pending audit should fail 50004, got %v", err)
	}
}

func TestVerifyRoleDenied(t *testing.T) {
	slotId := newSlotRow(t, context.Background(), consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	appId := directApp(t, context.Background(), slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 服务人员(3)不可核销
	if _, err := VerifyAppointment(staffCtx(consts.RoleService, consts.ShopId), appId); err == nil || err.Error() != "40001" {
		t.Fatalf("service staff verify should fail 40001, got %v", err)
	}
	// 客服(4)不可核销
	if _, err := VerifyAppointment(staffCtx(consts.RoleSupport, consts.ShopId), appId); err == nil || err.Error() != "40001" {
		t.Fatalf("support staff verify should fail 40001, got %v", err)
	}
	// 店长(1)可以核销（前台/店长均可）
	if _, err := VerifyAppointment(staffCtx(consts.RoleManager, consts.ShopId), appId); err != nil {
		t.Fatalf("manager verify should succeed: %v", err)
	}
}

func TestVerifyShopForbidden(t *testing.T) {
	slotId := newSlotRow(t, context.Background(), consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	appId := directApp(t, context.Background(), slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	if _, err := VerifyAppointment(staffCtx(consts.RoleFront, "shop_002"), appId); err == nil || err.Error() != "50001" {
		t.Fatalf("cross-shop verify should fail 50001, got %v", err)
	}
}

func TestVerifyWrongStatus(t *testing.T) {
	ctx := staffCtx(consts.RoleFront, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	// 待支付(1) 不可核销 → 50003
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusPending)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	if _, err := VerifyAppointment(ctx, appId); err == nil || err.Error() != "50003" {
		t.Fatalf("pending verify should fail 50003, got %v", err)
	}

	// 取消成功(4) 不可核销 → 50003
	appId2 := directApp(t, ctx, slotId, "pet_2003", "user_1002", consts.StatusCancelled)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId2).Delete() }()
	if _, err := VerifyAppointment(ctx, appId2); err == nil || err.Error() != "50003" {
		t.Fatalf("cancelled verify should fail 50003, got %v", err)
	}
}

func TestVerifyVsCancelConcurrent(t *testing.T) {
	// 取消与核销并发：仅先落库者成功
	ctx := staffCtx(consts.RoleFront, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 取消先落库（2→4）
	if _, err := g.DB().Model("appointment").
		Where("app_id", appId).
		Where("status", consts.StatusConfirmed).
		Data(g.Map{"status": consts.StatusCancelled, "updated_at": time.Now()}).
		Update(); err != nil {
		t.Fatal(err)
	}
	// 核销按最新状态失败 → 50003
	if _, err := VerifyAppointment(ctx, appId); err == nil || err.Error() != "50003" {
		t.Fatalf("verify after cancel should fail 50003, got %v", err)
	}
	if s, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status"); s.Int() != consts.StatusCancelled {
		t.Fatalf("status should stay cancelled, got %d", s.Int())
	}
}
