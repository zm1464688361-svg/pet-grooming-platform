package shop

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
)

func appFieldVal(t *testing.T, appId, field string) interface{} {
	t.Helper()
	v, err := g.DB().Model("appointment").Where("app_id", appId).Value(field)
	if err != nil {
		t.Fatalf("query %s: %v", field, err)
	}
	if v == nil {
		return nil
	}
	return v.Val()
}

func TestServiceProgressFlow(t *testing.T) {
	ctx := staffCtx(consts.RoleService, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusVerified)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 登记 5→6
	res, err := StartService(ctx, appId)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Status != consts.StatusInService {
		t.Fatalf("status should be 6, got %d", res.Status)
	}
	if appFieldVal(t, appId, "service_start_at") == nil {
		t.Fatal("service_start_at should be set")
	}

	// 重复登记开始 → 幂等提示"已登记开始"
	res2, err := StartService(ctx, appId)
	if err != nil {
		t.Fatalf("repeat start: %v", err)
	}
	if res2.Message != "已登记开始" {
		t.Fatalf("unexpected repeat message: %s", res2.Message)
	}

	// 跳级：未开始时登记完成 → 50009（此处已开始，先验证另一单跳级）
	appId2 := directApp(t, ctx, slotId, "pet_2003", "user_1002", consts.StatusVerified)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId2).Delete() }()
	if _, err := FinishService(ctx, appId2); err == nil || err.Error() != "50009" {
		t.Fatalf("skip to finish should fail 50009, got %v", err)
	}

	// 登记 6→7
	res3, err := FinishService(ctx, appId)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if res3.Status != consts.StatusFinished {
		t.Fatalf("status should be 7, got %d", res3.Status)
	}
	if appFieldVal(t, appId, "service_finish_at") == nil {
		t.Fatal("service_finish_at should be set")
	}

	// 重复点击完成 → 幂等提示"已完成登记"
	res4, err := FinishService(ctx, appId)
	if err != nil {
		t.Fatalf("repeat finish: %v", err)
	}
	if res4.Message != "已完成登记" {
		t.Fatalf("unexpected repeat finish message: %s", res4.Message)
	}

	// oper_log START/FINISH
	for _, action := range []string{"START", "FINISH"} {
		n, _ := g.DB().Model("oper_log").Where("app_id", appId).Where("action", action).Count()
		if n != 1 {
			t.Fatalf("%s oper_log missing", action)
		}
	}
}

func TestServiceNotVerified(t *testing.T) {
	ctx := staffCtx(consts.RoleService, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	// 未核销(2) 直接登记开始 → 50009
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	if _, err := StartService(ctx, appId); err == nil || err.Error() != "50009" {
		t.Fatalf("start before verify should fail 50009, got %v", err)
	}
}

func TestServiceStatus3Blocking(t *testing.T) {
	ctx := staffCtx(consts.RoleService, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	// 已核销(5)目标单 + 同时段 status=3 → 50004
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusVerified)
	appAudit := directApp(t, ctx, slotId, "pet_2003", "user_1002", consts.StatusCancelAudit)
	defer func() {
		_, _ = g.DB().Model("appointment").WhereIn("app_id", g.Slice{appId, appAudit}).Delete()
	}()
	if _, err := StartService(ctx, appId); err == nil || err.Error() != "50004" {
		t.Fatalf("start with audit pending should fail 50004, got %v", err)
	}
}

func TestServiceRoleDenied(t *testing.T) {
	slotId := newSlotRow(t, context.Background(), consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	appId := directApp(t, context.Background(), slotId, "pet_2001", "user_1001", consts.StatusVerified)
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	// 前台(2)不可登记服务
	if _, err := StartService(staffCtx(consts.RoleFront, consts.ShopId), appId); err == nil || err.Error() != "40001" {
		t.Fatalf("front start should fail 40001, got %v", err)
	}
	// 店长(1)可登记（服务人员/店长均可）
	if _, err := StartService(staffCtx(consts.RoleManager, consts.ShopId), appId); err != nil {
		t.Fatalf("manager start should succeed: %v", err)
	}
}

func TestServiceShopForbidden(t *testing.T) {
	slotId := newSlotRow(t, context.Background(), consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	appId := directApp(t, context.Background(), slotId, "pet_2001", "user_1001", consts.StatusVerified)
	defer cleanupSlot(t, context.Background(), slotId)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	if _, err := StartService(staffCtx(consts.RoleService, "shop_002"), appId); err == nil || err.Error() != "50001" {
		t.Fatalf("cross-shop start should fail 50001, got %v", err)
	}
}

func TestServiceFinalStateRejected(t *testing.T) {
	ctx := staffCtx(consts.RoleService, consts.ShopId)
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(72*time.Hour), time.Now().Add(73*time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	// 未到店(8) 不可登记服务 → 50003
	appId := directApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusNoShow)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	if _, err := StartService(ctx, appId); err == nil || err.Error() != "50003" {
		t.Fatalf("noshow start should fail 50003, got %v", err)
	}
}
