// Package pay 支付分支补充测试（守卫与幂等路径）。
package pay

import (
	"context"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
	"pet-grooming/internal/logic/user"
)

// makePendingOrder 建一个 status=1 的待支付订单（pet_2001 + 未来时段）。
func makePendingOrder(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()
	slotId := newSlot(t, ctx)
	r, err := user.Book(ctx, &user.BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: "px-" + guid.S()})
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	return slotId, r.AppId
}

// delPayLock 清除发起锁，便于重复发起场景。
func delPayLock(ctx context.Context, appId string) {
	_, _ = g.Redis().Do(ctx, "DEL", payLockKey(appId))
}

func TestInitiatePayGuards(t *testing.T) {
	ctx := context.Background()
	// 不存在的订单 → 40001
	if _, err := InitiatePay(ctx, "user_1001", "no_such_app"); err == nil || err.Error() != "40001" {
		t.Fatalf("unknown app should 40001, got %v", err)
	}
	// 非本人订单 → 40001
	slotId, appId := makePendingOrder(t, ctx)
	defer func() {
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		cleanup(t, ctx, slotId, "", "", "")
	}()
	if _, err := InitiatePay(ctx, "user_9999", appId); err == nil || err.Error() != "40001" {
		t.Fatalf("other user should 40001, got %v", err)
	}
	// 超时订单（lock_expire_at 已过）→ 40005
	_, _ = g.DB().Model("appointment").Data(g.Map{"lock_expire_at": time.Now().Add(-time.Minute)}).Where("app_id", appId).Update()
	if _, err := InitiatePay(ctx, "user_1001", appId); err == nil || err.Error() != "40005" {
		t.Fatalf("expired should 40005, got %v", err)
	}
	// 恢复未过期，发起两次：第二次 30s 锁内 → 40008
	_, _ = g.DB().Model("appointment").Data(g.Map{"lock_expire_at": time.Now().Add(5 * time.Minute)}).Where("app_id", appId).Update()
	delPayLock(ctx, appId)
	pid1, err := InitiatePay(ctx, "user_1001", appId)
	if err != nil {
		t.Fatalf("first initiate: %v", err)
	}
	if _, err := InitiatePay(ctx, "user_1001", appId); err == nil || err.Error() != "40008" {
		t.Fatalf("second initiate within lock should 40008, got %v", err)
	}
	delPayLock(ctx, appId)
	defer func() {
		_, _ = g.DB().Model("payment").Where("payment_id", pid1).Delete()
		_, _ = g.DB().Model("payment_log").Where("app_id", appId).Delete()
	}()
	// 模拟支付失败回调后重新发起 → 复用同一支付单并重置为待支付
	if err := HandleNotify(ctx, NotifyInput{PaymentId: pid1, TransactionId: "txn_reuse_" + guid.S(), Success: false, Amount: 9900}); err != nil {
		t.Fatalf("failed notify: %v", err)
	}
	pid2, err := InitiatePay(ctx, "user_1001", appId)
	if err != nil {
		t.Fatalf("re-initiate: %v", err)
	}
	if pid2 != pid1 {
		t.Fatalf("re-initiate should reuse payment: %s vs %s", pid2, pid1)
	}
	v, _ := g.DB().Model("payment").Where("payment_id", pid1).Value("status")
	if v.Int() != consts.PayStatusUnpaid {
		t.Fatalf("reused payment should be unpaid, got %v", v)
	}
}

func TestNotifyGuardsAndIdempotent(t *testing.T) {
	ctx := context.Background()
	slotId, appId := makePendingOrder(t, ctx)
	defer cleanup(t, ctx, slotId, appId, "", "")

	// 不存在的支付单回调 → 错误
	if err := HandleNotify(ctx, NotifyInput{PaymentId: "no_such_pay", TransactionId: "txn_g1_" + guid.S(), Success: true, Amount: 9900}); err == nil {
		t.Fatal("unknown payment should fail")
	}
	// 发起支付
	paymentId, err := InitiatePay(ctx, "user_1001", appId)
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	defer func() {
		_, _ = g.DB().Model("payment").Where("payment_id", paymentId).Delete()
		_, _ = g.DB().Model("payment_log").Where("app_id", appId).Delete()
	}()
	txn := "txn_g2_" + guid.S()
	// 失败回调：订单保持待支付，可重新发起
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn, Success: false, Amount: 9900}); err != nil {
		t.Fatalf("failed notify: %v", err)
	}
	if st := statusOf(t, appId); st != consts.StatusPending {
		t.Fatalf("failed notify must not confirm order, got %d", st)
	}
	// 重置支付单为待支付（重新发起路径）
	_, _ = g.DB().Model("payment").Data(g.Map{"status": consts.PayStatusUnpaid, "transaction_id": ""}).Where("payment_id", paymentId).Update()
	// 同 transactionId 重复成功回调：首次成功，第二次 Redis 幂等返回 nil
	txn2 := "txn_g3_" + guid.S()
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn2, Success: true, Amount: 9900}); err != nil {
		t.Fatalf("success notify: %v", err)
	}
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn2, Success: true, Amount: 9900}); err != nil {
		t.Fatalf("duplicate notify (redis guard) should be idempotent: %v", err)
	}
	if st := statusOf(t, appId); st != consts.StatusConfirmed {
		t.Fatalf("order should be confirmed, got %d", st)
	}
	// 清 Redis 幂等锁后同号再次回调 → DB 兜底幂等（已支付同号）
	_, _ = g.Redis().Do(ctx, "DEL", notifyLockKey(txn2))
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn2, Success: true, Amount: 9900}); err != nil {
		t.Fatalf("duplicate notify (db guard) should be idempotent: %v", err)
	}
	// 异号成功回调（已支付）→ 异号重复支付异常单（不动订单状态）
	txn3 := "txn_g4_" + guid.S()
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn3, Success: true, Amount: 9900}); err != nil {
		t.Fatalf("different txn notify: %v", err)
	}
	if n := excCount(t, appId); n != 1 {
		t.Fatalf("expect 1 duplicate exception, got %d", n)
	}
	if st := statusOf(t, appId); st != consts.StatusConfirmed {
		t.Fatalf("duplicate pay must not change status, got %d", st)
	}
	defer func() { _, _ = g.DB().Model("pay_exception").Where("transaction_id", txn3).Delete() }()
	_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId))
}
