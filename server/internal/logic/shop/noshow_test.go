package shop

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
)

// endedSlot 创建已结束时段。
func endedSlot(t *testing.T, ctx context.Context) string {
	t.Helper()
	return newSlotRow(t, ctx, consts.ShopId, time.Now().Add(-3*time.Hour), time.Now().Add(-1*time.Hour), consts.SlotActive)
}

func TestNoShowSuccess(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	slotId := endedSlot(t, ctx)
	defer cleanupSlot(t, ctx, slotId)

	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	_, _ = quota.Occupy(ctx, slotId, "pet_2001", consts.MaxCapacity)
	defer func() { _, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId) }()

	res, err := MarkNoShow(ctx, appId, "未到店，电话无人接听")
	if err != nil {
		t.Fatalf("noshow: %v", err)
	}
	if res.Status != consts.StatusNoShow || res.Message != "已标记未到店，本次不自动退款" {
		t.Fatalf("unexpected result: %+v", res)
	}

	// 终态字段：noshow_at/noshow_reason
	v, _ := g.DB().Model("appointment").Where("app_id", appId).Value("noshow_at")
	if v.IsNil() {
		t.Fatal("noshow_at should be set")
	}
	if v2, _ := g.DB().Model("appointment").Where("app_id", appId).Value("noshow_reason"); v2.String() == "" {
		t.Fatal("noshow_reason should be set")
	}

	// 名额不释放（永久占用）
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity-1 {
		t.Fatalf("quota should stay occupied, remaining=%d", remaining)
	}

	// oper_log NOSHOW
	n, _ := g.DB().Model("oper_log").Where("app_id", appId).Where("action", "NOSHOW").Count()
	if n != 1 {
		t.Fatal("NOSHOW oper_log missing")
	}

	// 重复标记 → 幂等提示
	res2, err := MarkNoShow(ctx, appId, "再标记一次")
	if err != nil {
		t.Fatalf("repeat noshow: %v", err)
	}
	if res2.Message != "已标记未到店" {
		t.Fatalf("unexpected repeat message: %s", res2.Message)
	}
}

func TestNoShowReasonRequired(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	slotId := endedSlot(t, ctx)
	defer cleanupSlot(t, ctx, slotId)

	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 原因为空 → 50006
	if _, err := MarkNoShow(ctx, appId, "  "); err == nil || err.Error() != "50006" {
		t.Fatalf("empty reason should fail 50006, got %v", err)
	}
	// DB 不得写入默认值
	if v, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status"); v.Int() != consts.StatusConfirmed {
		t.Fatal("status must not change on invalid request")
	}
}

func TestNoShowSlotNotEnded(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	// 时段尚未结束 → 50005
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	if _, err := MarkNoShow(ctx, appId, "未到店"); err == nil || err.Error() != "50005" {
		t.Fatalf("slot not ended should fail 50005, got %v", err)
	}
}

func TestNoShowRoleAndShop(t *testing.T) {
	slotId := endedSlot(t, context.Background())
	appId := directApp(t, context.Background(), slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 前台(2)不可标记未到店 → 40001
	if _, err := MarkNoShow(staffCtx(consts.RoleFront, consts.ShopId), appId, "x"); err == nil || err.Error() != "40001" {
		t.Fatalf("front noshow should fail 40001, got %v", err)
	}
	// 客服(4)不可标记未到店 → 40001
	if _, err := MarkNoShow(staffCtx(consts.RoleSupport, consts.ShopId), appId, "x"); err == nil || err.Error() != "40001" {
		t.Fatalf("support noshow should fail 40001, got %v", err)
	}
	// 跨店 → 50001
	if _, err := MarkNoShow(staffCtx(consts.RoleManager, "shop_002"), appId, "x"); err == nil || err.Error() != "50001" {
		t.Fatalf("cross-shop noshow should fail 50001, got %v", err)
	}
	// 店长可标记
	if _, err := MarkNoShow(staffCtx(consts.RoleManager, consts.ShopId), appId, "未到店"); err != nil {
		t.Fatalf("manager noshow should succeed: %v", err)
	}
}

func TestNoShowWrongStatus(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	slotId := endedSlot(t, ctx)
	defer cleanupSlot(t, ctx, slotId)

	// 状态 5 已核销 → 不可标记未到店（50003）
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusVerified)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	if _, err := MarkNoShow(ctx, appId, "未到店"); err == nil || err.Error() != "50003" {
		t.Fatalf("verified noshow should fail 50003, got %v", err)
	}

	// 同时段 status=3 → 50004
	appId2 := directApp(t, ctx, slotId, "pet_2003", "user_1002", consts.StatusConfirmed)
	appAudit := directApp(t, ctx, slotId, "pet_2005", "user_1003", consts.StatusCancelAudit)
	defer func() {
		_, _ = g.DB().Model("appointment").WhereIn("app_id", g.Slice{appId2, appAudit}).Delete()
	}()
	if _, err := MarkNoShow(ctx, appId2, "未到店"); err == nil || err.Error() != "50004" {
		t.Fatalf("noshow with audit pending should fail 50004, got %v", err)
	}
}
