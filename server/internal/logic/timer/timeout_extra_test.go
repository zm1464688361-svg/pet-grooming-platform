// Package timer 定时任务补充测试（cron 回调链路与名额释放）。
package timer

import (
	"context"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
)

// TestStartRegistersCron Start 可重复注册（幂等测试，不依赖真实间隔执行）。
func TestStartRegistersCron(t *testing.T) {
	Start(context.Background())
	// 重复注册同任务名不应 panic（gcron 覆盖式注册）
	Start(context.Background())
}

// TestCronCallbackClosesExpired 用 1 秒间隔触发回调体：关闭超时订单并释放名额。
func TestCronCallbackClosesExpired(t *testing.T) {
	ctx := context.Background()
	slotId := "slot_tm_" + guid.S()
	_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	defer func() {
		_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Delete()
		_, _ = g.DB().Model("oper_log").Where("app_id LIKE ?", "app_tm_%").Delete()
		_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	}()

	appId := insertTestApp(t, ctx, slotId, "pet_2001", "user_1001", time.Now().Add(-time.Minute))
	registerTimeoutJob(ctx, "@every 1s", "timeout_close_test_"+guid.S())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, err := g.DB().Model("appointment").Where("app_id", appId).Value("status")
		if err != nil {
			t.Fatalf("query status: %v", err)
		}
		if v.Int() == consts.StatusTimeoutClosed {
			// 名额已释放
			rem, err := quota.Remaining(ctx, slotId, consts.MaxCapacity)
			if err != nil {
				t.Fatalf("remaining: %v", err)
			}
			if rem != consts.MaxCapacity {
				t.Fatalf("quota should be released, remaining=%d", rem)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("cron callback did not close expired order in time")
}

// TestCloseSkipsPaidAlarm 已支付但超时订单 → 跳过关闭（告警分支）。
func TestCloseSkipsPaidAlarm(t *testing.T) {
	ctx := context.Background()
	slotId := "slot_tm2_" + guid.S()
	defer func() {
		_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Delete()
		_, _ = g.DB().Model("payment").Where("app_id LIKE ?", "app_tm2_%").Delete()
		_, _ = g.DB().Model("oper_log").Where("app_id LIKE ?", "app_tm2_%").Delete()
		_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	}()

	appId := insertTestApp(t, ctx, slotId, "pet_2001", "user_1001", time.Now().Add(-time.Minute))
	_, err := g.DB().Model("payment").Data(g.Map{
		"payment_id": "pay_tm2_" + guid.S(), "app_id": appId, "amount": consts.PriceCent,
		"status": 2, "created_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}
	n, err := CloseTimeoutOnce(ctx)
	if err != nil {
		t.Fatalf("close once: %v", err)
	}
	// 该订单被跳过，状态保持 1
	v, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status")
	if v.Int() != consts.StatusPending {
		t.Fatalf("paid-but-expired order must be skipped, got status=%d closed=%d", v.Int(), n)
	}
}
