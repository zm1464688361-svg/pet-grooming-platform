// Package quota 名额分支补充测试（负数告警、有预约对账重建）。
package quota

import (
	"context"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
)

// TestReleaseNegativeAlarm 造负计数后释放 → 告警错误（重建为 0）。
func TestReleaseNegativeAlarm(t *testing.T) {
	ctx := context.Background()
	slotId := newTestSlotId()
	defer cleanKeys(t, slotId)

	_, _ = g.Redis().Do(ctx, "SET", countKey(slotId), -1)
	_, _ = g.Redis().Do(ctx, "SADD", petsKey(slotId), "pet_2001")

	if err := Release(ctx, slotId, "pet_2001"); err == nil {
		t.Fatal("negative count release should alarm")
	}
	// 告警路径已重建计数为 0
	rem, err := Remaining(ctx, slotId, consts.MaxCapacity)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if rem != consts.MaxCapacity {
		t.Fatalf("after alarm rebuild remaining should be %d, got %d", consts.MaxCapacity, rem)
	}
}

// TestRebuildQuotaWithOrders 有有效预约（status=2）时对账重建恢复计数与集合。
func TestRebuildQuotaWithOrders(t *testing.T) {
	ctx := context.Background()
	slotId := "slot_q_" + guid.S()
	defer func() {
		_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Delete()
		cleanKeys(t, slotId)
	}()

	start := time.Now().Add(3 * time.Hour)
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id": slotId, "shop_id": consts.ShopId,
		"start_time": start, "end_time": start.Add(time.Hour),
		"max_capacity": consts.MaxCapacity, "status": consts.SlotActive,
		"created_by": "manager_01", "created_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert slot: %v", err)
	}
	appId := "app_q_" + guid.S()
	_, err = g.DB().Model("appointment").Data(g.Map{
		"app_id": appId, "user_id": "user_1001", "pet_id": "pet_2001", "shop_id": consts.ShopId,
		"item_id": consts.ItemId, "slot_id": slotId, "price": 9900,
		"status": consts.StatusConfirmed, "lock_expire_at": time.Now().Add(5 * time.Minute),
		"created_at": time.Now(), "updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert appointment: %v", err)
	}
	// 污染 Redis
	_, _ = g.Redis().Do(ctx, "SET", countKey(slotId), 99)
	if n, err := g.DB().Model("appointment").Where("slot_id", slotId).Count(); err != nil || n != 1 {
		t.Fatalf("appointment insert sanity: n=%d err=%v", n, err)
	}

	if err := RebuildQuota(ctx, slotId); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rem, err := Remaining(ctx, slotId, consts.MaxCapacity)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if rem != consts.MaxCapacity-1 {
		t.Fatalf("rebuild with 1 valid order: remaining should be %d, got %d", consts.MaxCapacity-1, rem)
	}
	// 宠物集合恢复
	ok, err := g.Redis().Do(ctx, "SISMEMBER", petsKey(slotId), "pet_2001")
	if err != nil || ok.Int() != 1 {
		t.Fatalf("pet set should be rebuilt, ok=%v err=%v", ok, err)
	}

	// 状态改为服务完成(7)后重建：名额仍应占用（红线 4.1.4）
	if _, err := g.DB().Model("appointment").Data(g.Map{"status": consts.StatusFinished}).Where("app_id", appId).Update(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := RebuildQuota(ctx, slotId); err != nil {
		t.Fatalf("rebuild after finish: %v", err)
	}
	rem2, err := Remaining(ctx, slotId, consts.MaxCapacity)
	if err != nil {
		t.Fatalf("remaining after finish: %v", err)
	}
	if rem2 != consts.MaxCapacity-1 {
		t.Fatalf("finished order should still occupy quota: remaining should be %d, got %d", consts.MaxCapacity-1, rem2)
	}
}
