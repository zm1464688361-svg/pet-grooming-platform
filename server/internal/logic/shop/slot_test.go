package shop

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
)

func staffCtx(role int, shopId string) context.Context {
	return auth.WithStaff(context.Background(), &auth.StaffIdentity{
		StaffId: "staff_test", ShopId: shopId, StaffName: "测试", Role: role,
	})
}

func newSlotRow(t *testing.T, ctx context.Context, shopId string, start, end time.Time, status int) string {
	t.Helper()
	slotId := "slot_b_" + guid.S()
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      shopId,
		"start_time":   start,
		"end_time":     end,
		"max_capacity": consts.MaxCapacity,
		"status":       status,
		"created_by":   "manager_01",
		"created_at":   time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert slot: %v", err)
	}
	return slotId
}

func cleanupSlot(t *testing.T, ctx context.Context, slotId string) {
	t.Helper()
	_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Delete()
	_, _ = g.DB().Model("oper_log").Where("app_id", slotId).Delete()
	_, _ = g.DB().Model("time_slot").Where("slot_id", slotId).Delete()
}

func slotStatus(t *testing.T, slotId string) int {
	t.Helper()
	v, err := g.DB().Model("time_slot").Where("slot_id", slotId).Value("status")
	if err != nil || v == nil {
		t.Fatalf("query slot status: %v", err)
	}
	return v.Int()
}

func operLogCount(t *testing.T, slotId, action string) int {
	t.Helper()
	v, err := g.DB().Model("oper_log").Where("app_id", slotId).Where("action", action).Count()
	if err != nil {
		t.Fatalf("query oper_log: %v", err)
	}
	return v
}

func TestCreateSlotByManager(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	start := time.Now().Add(48 * time.Hour)
	slotId, err := CreateSlot(ctx, start, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer cleanupSlot(t, ctx, slotId)

	// 容量固定 3（前端传值被忽略）
	v, _ := g.DB().Model("time_slot").Where("slot_id", slotId).Value("max_capacity")
	if v.Int() != consts.MaxCapacity {
		t.Fatalf("capacity must be fixed 3, got %d", v.Int())
	}
	if slotStatus(t, slotId) != consts.SlotActive {
		t.Fatal("new slot should be active(1)")
	}
	if operLogCount(t, slotId, "slot_create") != 1 {
		t.Fatal("slot_create oper_log missing")
	}
}

func TestCreateSlotRoleDenied(t *testing.T) {
	ctx := staffCtx(consts.RoleFront, consts.ShopId)
	start := time.Now().Add(48 * time.Hour)
	if _, err := CreateSlot(ctx, start, start.Add(time.Hour)); err == nil || err.Error() != "40001" {
		t.Fatalf("front staff should be denied 40001, got %v", err)
	}
}

func TestCreateSlotInvalidTime(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	now := time.Now()
	// 结束早于开始
	if _, err := CreateSlot(ctx, now.Add(48*time.Hour), now.Add(47*time.Hour)); err == nil || err.Error() != "40002" {
		t.Fatalf("end<start should fail 40002, got %v", err)
	}
	// 开始早于当前时间
	if _, err := CreateSlot(ctx, now.Add(-time.Hour), now.Add(time.Hour)); err == nil || err.Error() != "40002" {
		t.Fatalf("past start should fail 40002, got %v", err)
	}
	// 与现有时段重叠（自建基准时段，避免依赖 seed 相对时间）
	base, err := CreateSlot(ctx, now.Add(72*time.Hour), now.Add(73*time.Hour))
	if err != nil {
		t.Fatalf("create base: %v", err)
	}
	defer cleanupSlot(t, ctx, base)
	if _, err := CreateSlot(ctx, now.Add(72*time.Hour).Add(30*time.Minute), now.Add(74*time.Hour)); err == nil || err.Error() != "40002" {
		t.Fatalf("overlap should fail 40002, got %v", err)
	}
}

func TestPauseResumeSlot(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	start := time.Now().Add(48 * time.Hour)
	slotId, err := CreateSlot(ctx, start, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer cleanupSlot(t, ctx, slotId)

	// 暂停
	if err := PauseSlot(ctx, slotId); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if s := slotStatus(t, slotId); s != consts.SlotPaused {
		t.Fatalf("paused status should be 2, got %d", s)
	}
	// 恢复
	if err := ResumeSlot(ctx, slotId); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if s := slotStatus(t, slotId); s != consts.SlotActive {
		t.Fatalf("resumed status should be 1, got %d", s)
	}
	if operLogCount(t, slotId, "slot_pause") != 1 || operLogCount(t, slotId, "slot_resume") != 1 {
		t.Fatal("pause/resume oper_log missing")
	}
}

func TestPauseSlotWithBooking(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	start := time.Now().Add(48 * time.Hour)
	slotId, err := CreateSlot(ctx, start, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer cleanupSlot(t, ctx, slotId)

	// 存在有效预约(status=2) → 禁止暂停 50002
	_, err = g.DB().Model("appointment").Data(g.Map{
		"app_id":     "app_b_" + guid.S(),
		"user_id":    "user_1001",
		"pet_id":     "pet_2001",
		"slot_id":    slotId,
		"shop_id":    consts.ShopId,
		"item_id":    consts.ItemId,
		"price":      consts.PriceCent,
		"status":     consts.StatusConfirmed,
		"created_at": time.Now(),
		"updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert appointment: %v", err)
	}

	if err := PauseSlot(ctx, slotId); err == nil || err.Error() != "50002" {
		t.Fatalf("pause with booking should fail 50002, got %v", err)
	}
	if err := DeleteSlot(ctx, slotId); err == nil || err.Error() != "50002" {
		t.Fatalf("delete with booking should fail 50002, got %v", err)
	}
	_, err = g.DB().Model("time_slot").Data(g.Map{
		"start_time": start.Add(2 * time.Hour),
		"end_time":   start.Add(3 * time.Hour),
	}).Where("slot_id", slotId).Update()
	if err != nil {
		t.Fatal(err)
	}
	// 更新时间同样 50002（沿用存在有效预约的校验）——直接调用逻辑层
	if err := UpdateSlotTime(ctx, slotId, start.Add(3*time.Hour), start.Add(4*time.Hour)); err == nil || err.Error() != "50002" {
		t.Fatalf("update with booking should fail 50002, got %v", err)
	}
}

func TestUpdateSlotTime(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	start := time.Now().Add(48 * time.Hour)
	slotId, err := CreateSlot(ctx, start, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer cleanupSlot(t, ctx, slotId)

	if err := UpdateSlotTime(ctx, slotId, start.Add(5*time.Hour), start.Add(6*time.Hour)); err != nil {
		t.Fatalf("update: %v", err)
	}
	v, _ := g.DB().Model("time_slot").Where("slot_id", slotId).Value("start_time")
	if !v.Time().After(time.Now().Add(52 * time.Hour)) { // 更新后 start ≈ now+53h
		t.Fatalf("start_time not updated: %v", v.Time())
	}
	if operLogCount(t, slotId, "slot_update") != 1 {
		t.Fatal("slot_update oper_log missing")
	}
}

func TestDeleteSlot(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	start := time.Now().Add(48 * time.Hour)
	slotId, err := CreateSlot(ctx, start, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer cleanupSlot(t, ctx, slotId)

	if err := DeleteSlot(ctx, slotId); err != nil {
		t.Fatalf("delete: %v", err)
	}
	n, _ := g.DB().Model("time_slot").Where("slot_id", slotId).Count()
	if n != 0 {
		t.Fatal("slot should be deleted")
	}
}

func TestShopIsolation(t *testing.T) {
	// 其他门店员工：无任何本店数据权限
	ctx := staffCtx(consts.RoleManager, "shop_002")
	start := time.Now().Add(48 * time.Hour)

	// 列表隔离：shop_002 员工看不到 shop_001 的时段
	list, err := ListShopSlots(ctx, 1, 20, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, it := range list {
		if it.SlotId == "slot_01" || it.SlotId == "slot_02" || it.SlotId == "slot_03" {
			t.Fatal("shop isolation broken: other shop's slot returned")
		}
	}

	// 操作他店时段 → 50001
	slotId := newSlotRow(t, context.Background(), consts.ShopId, start, start.Add(time.Hour), consts.SlotActive)
	defer cleanupSlot(t, context.Background(), slotId)

	if err := PauseSlot(ctx, slotId); err == nil || err.Error() != "50001" {
		t.Fatalf("cross-shop pause should fail 50001, got %v", err)
	}
}
