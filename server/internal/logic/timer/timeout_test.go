package timer

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

// insertTestApp 插入测试待支付订单并占用名额。
func insertTestApp(t *testing.T, ctx context.Context, slotId, petId, userId string, lockExpire time.Time) string {
	t.Helper()
	appId := "app_t_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":         appId,
		"user_id":        userId,
		"pet_id":         petId,
		"slot_id":        slotId,
		"shop_id":        consts.ShopId,
		"item_id":        consts.ItemId,
		"price":          consts.PriceCent,
		"status":         consts.StatusPending,
		"lock_expire_at": lockExpire,
		"created_at":     time.Now(),
		"updated_at":     time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert appointment: %v", err)
	}
	if _, err := quota.Occupy(ctx, slotId, petId, consts.MaxCapacity); err != nil {
		t.Fatalf("occupy: %v", err)
	}
	return appId
}

func insertTestSlot(t *testing.T, ctx context.Context) string {
	t.Helper()
	slotId := "slot_t_" + guid.S()
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      consts.ShopId,
		"start_time":   time.Now().Add(3 * time.Hour),
		"end_time":     time.Now().Add(4 * time.Hour),
		"max_capacity": consts.MaxCapacity,
		"status":       consts.SlotActive,
		"created_by":   "manager_01",
		"created_at":   time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert slot: %v", err)
	}
	return slotId
}

func cleanupTestSlot(t *testing.T, ctx context.Context, slotId string, appIds ...string) {
	t.Helper()
	_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Delete()
	for _, appId := range appIds {
		_, _ = g.DB().Model("oper_log").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("payment").Where("app_id", appId).Delete()
	}
	_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	_, _ = g.DB().Model("time_slot").Where("slot_id", slotId).Delete()
}

func appStatus(t *testing.T, appId string) int {
	t.Helper()
	res, err := g.DB().Model("appointment").Where("app_id", appId).Value("status")
	if err != nil {
		t.Fatalf("query status: %v", err)
	}
	if res == nil {
		t.Fatalf("appointment %s not found", appId)
	}
	return res.Int()
}

func TestCloseExpiredPendingOnly(t *testing.T) {
	ctx := context.Background()
	slotId := insertTestSlot(t, ctx)
	defer cleanupTestSlot(t, ctx, slotId)

	// A 已超时（lock_expire_at < now），B 未超时；同时段各占 1 名额
	appA := insertTestApp(t, ctx, slotId, "pet_2001", "user_1001", time.Now().Add(-1*time.Minute))
	appB := insertTestApp(t, ctx, slotId, "pet_2003", "user_1002", time.Now().Add(5*time.Minute))
	defer cleanupTestSlot(t, ctx, slotId, appA, appB)

	closed, err := CloseTimeoutOnce(ctx)
	if err != nil {
		t.Fatalf("CloseTimeoutOnce: %v", err)
	}
	if closed < 1 {
		t.Fatalf("should close at least 1 order, closed=%d", closed)
	}

	// A 被关闭为 9（终态），B 保持待支付
	if s := appStatus(t, appA); s != consts.StatusTimeoutClosed {
		t.Fatalf("expired order should be 9, got %d", s)
	}
	if s := appStatus(t, appB); s != consts.StatusPending {
		t.Fatalf("valid order should stay 1, got %d", s)
	}

	// 仅释放本单名额：剩余 = 3 - 1 = 2
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity-1 {
		t.Fatalf("remaining should be %d, got %d", consts.MaxCapacity-1, remaining)
	}

	// 同宠物重约同 slot（软唯一键不再占用终态单）
	if _, err := quota.Occupy(ctx, slotId, "pet_2001", consts.MaxCapacity); err != nil {
		t.Fatalf("re-occupy after release should succeed: %v", err)
	}
}

func TestCloseSkipsPaidOrder(t *testing.T) {
	ctx := context.Background()
	slotId := insertTestSlot(t, ctx)
	defer cleanupTestSlot(t, ctx, slotId)

	// 已超时但支付日志已成功（回调并发竞态窗口）→ 不关闭
	appId := insertTestApp(t, ctx, slotId, "pet_2005", "user_1003", time.Now().Add(-1*time.Minute))
	defer cleanupTestSlot(t, ctx, slotId, appId)
	_, err := g.DB().Model("payment").Data(g.Map{
		"payment_id": "pay_t_" + guid.S(),
		"app_id":     appId,
		"amount":     consts.PriceCent,
		"status":     consts.PayStatusPaid,
		"created_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	if _, err := CloseTimeoutOnce(ctx); err != nil {
		t.Fatalf("CloseTimeoutOnce: %v", err)
	}

	if s := appStatus(t, appId); s != consts.StatusPending {
		t.Fatalf("paid order must not be closed, got %d", s)
	}
}
