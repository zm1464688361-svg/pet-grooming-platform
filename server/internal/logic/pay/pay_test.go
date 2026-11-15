package pay

import (
	"context"
	"sync"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
	"pet-grooming/internal/logic/user"
)

// 依赖 seed 数据与本地 MySQL/Redis。

func newSlot(t *testing.T, ctx context.Context) string {
	t.Helper()
	slotId := "slot_p_" + guid.S()
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

func cleanup(t *testing.T, ctx context.Context, slotId, appId, paymentId, transactionId string) {
	t.Helper()
	if appId != "" {
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("oper_log").Where("app_id", appId).Delete()
	}
	if paymentId != "" {
		_, _ = g.DB().Model("payment").Where("payment_id", paymentId).Delete()
		_, _ = g.DB().Model("payment_log").Where("payment_id", paymentId).Delete()
	}
	if transactionId != "" {
		_, _ = g.DB().Model("pay_exception").Where("transaction_id", transactionId).Delete()
	}
	_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId, payLockKey(appId), notifyLockKey(transactionId))
	_, _ = g.DB().Model("time_slot").Where("slot_id", slotId).Delete()
}

func statusOf(t *testing.T, appId string) int {
	t.Helper()
	v, err := g.DB().Model("appointment").Where("app_id", appId).Value("status")
	if err != nil || v == nil {
		t.Fatalf("query app status: %v", err)
	}
	return v.Int()
}

func excCount(t *testing.T, appId string) int {
	t.Helper()
	v, err := g.DB().Model("pay_exception").Where("app_id", appId).Count()
	if err != nil {
		t.Fatalf("query exception: %v", err)
	}
	return v
}

func TestPayFullFlow(t *testing.T) {
	ctx := context.Background()
	slotId := newSlot(t, ctx)

	res, err := user.Book(ctx, &user.BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	appId := res.AppId
	transactionId := "txn_" + guid.S()
	defer cleanup(t, ctx, slotId, appId, "", transactionId)

	// 发起支付
	paymentId, err := InitiatePay(ctx, "user_1001", appId)
	if err != nil {
		t.Fatalf("initiate pay: %v", err)
	}

	// 回调成功（模拟渠道）
	if err := HandleNotify(ctx, NotifyInput{
		PaymentId:     paymentId,
		TransactionId: transactionId,
		Success:       true,
		Amount:        consts.PriceCent,
	}); err != nil {
		t.Fatalf("notify: %v", err)
	}

	// 订单变已确认(2)，支付单已支付(2)
	if s := statusOf(t, appId); s != consts.StatusConfirmed {
		t.Fatalf("order should be 2, got %d", s)
	}
	v, _ := g.DB().Model("payment").Where("payment_id", paymentId).Value("status")
	if v.Int() != consts.PayStatusPaid {
		t.Fatalf("payment should be paid, got %d", v.Int())
	}

	// 名额保持占用
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity-1 {
		t.Fatalf("quota should stay occupied, remaining=%d", remaining)
	}

	// 同号重复回调 → 幂等返回成功，无异常单
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: transactionId, Success: true, Amount: consts.PriceCent}); err != nil {
		t.Fatalf("idempotent notify should succeed: %v", err)
	}
	if n := excCount(t, appId); n != 0 {
		t.Fatalf("same transaction retry must not create exception, got %d", n)
	}
}

func TestPayDuplicateTransaction(t *testing.T) {
	ctx := context.Background()
	slotId := newSlot(t, ctx)

	res, _ := user.Book(ctx, &user.BookInput{UserId: "user_1001", PetId: "pet_2002", SlotId: slotId})
	appId := res.AppId
	paymentId, err := InitiatePay(ctx, "user_1001", appId)
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	txn1, txn2 := "txn_"+guid.S(), "txn_"+guid.S()
	defer cleanup(t, ctx, slotId, appId, paymentId, txn1)
	defer cleanup(t, ctx, slotId, appId, paymentId, txn2)

	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn1, Success: true, Amount: consts.PriceCent}); err != nil {
		t.Fatalf("first notify: %v", err)
	}
	// 异号第二次支付成功 → 支付异常单，订单保持 2
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn2, Success: true, Amount: consts.PriceCent}); err != nil {
		t.Fatalf("duplicate notify: %v", err)
	}

	if s := statusOf(t, appId); s != consts.StatusConfirmed {
		t.Fatalf("order should stay 2, got %d", s)
	}
	if n := excCount(t, appId); n != 1 {
		t.Fatalf("duplicate pay should create 1 exception, got %d", n)
	}
	v, _ := g.DB().Model("pay_exception").Where("app_id", appId).Value("exc_type")
	if v.Int() != consts.ExcTypeDuplicate {
		t.Fatalf("exception type should be duplicate(2), got %d", v.Int())
	}
}

func TestPayAfterClose(t *testing.T) {
	ctx := context.Background()
	slotId := newSlot(t, ctx)

	res, _ := user.Book(ctx, &user.BookInput{UserId: "user_1002", PetId: "pet_2003", SlotId: slotId})
	appId := res.AppId
	paymentId, _ := InitiatePay(ctx, "user_1002", appId)
	txn := "txn_" + guid.S()
	defer cleanup(t, ctx, slotId, appId, paymentId, txn)

	// 手动将订单置为超时关闭（模拟定时任务已关闭）
	_, err := g.DB().Model("appointment").
		Where("app_id", appId).
		Data(g.Map{"status": consts.StatusTimeoutClosed, "updated_at": time.Now()}).
		Update()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	_ = quota.Release(ctx, slotId, "pet_2003")

	// 关闭后支付成功 → 支付异常单（关闭后支付），订单保持 9，不释放名额
	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn, Success: true, Amount: consts.PriceCent}); err != nil {
		t.Fatalf("notify after close: %v", err)
	}

	if s := statusOf(t, appId); s != consts.StatusTimeoutClosed {
		t.Fatalf("closed order should stay 9, got %d", s)
	}
	if n := excCount(t, appId); n != 1 {
		t.Fatalf("closed-after-pay should create 1 exception, got %d", n)
	}
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity {
		t.Fatalf("quota should stay released, remaining=%d", remaining)
	}
}

func TestPayInitiateGuards(t *testing.T) {
	ctx := context.Background()
	slotId := newSlot(t, ctx)

	res, _ := user.Book(ctx, &user.BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	appId := res.AppId
	defer cleanup(t, ctx, slotId, appId, "", "")

	// 他人订单 → 40001
	if _, err := InitiatePay(ctx, "user_1002", appId); err == nil || err.Error() != "40001" {
		t.Fatalf("other user pay should fail 40001, got %v", err)
	}

	// 已确认订单 → 40009
	_, _ = g.DB().Model("appointment").Where("app_id", appId).Data(g.Map{"status": consts.StatusConfirmed}).Update()
	if _, err := InitiatePay(ctx, "user_1001", appId); err == nil || err.Error() != "40009" {
		t.Fatalf("confirmed order pay should fail 40009, got %v", err)
	}
	_, _ = g.DB().Model("appointment").Where("app_id", appId).Data(g.Map{"status": consts.StatusPending}).Update()

	// 并发点击 → 仅一个成功，其余 40008
	var mu sync.Mutex
	okCnt, fail8 := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := InitiatePay(ctx, "user_1001", appId)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				okCnt++
			} else if err.Error() == "40008" {
				fail8++
			}
		}()
	}
	wg.Wait()
	if okCnt != 1 || fail8 != 2 {
		t.Fatalf("concurrent initiate: want 1 ok / 2 fail(40008), got ok=%d fail=%d", okCnt, fail8)
	}
	// 释放支付锁，避免影响其他测试
	_, _ = g.Redis().Do(ctx, "DEL", payLockKey(appId))
}

func TestPayFailedNotify(t *testing.T) {
	ctx := context.Background()
	slotId := newSlot(t, ctx)

	res, _ := user.Book(ctx, &user.BookInput{UserId: "user_1001", PetId: "pet_2002", SlotId: slotId})
	appId := res.AppId
	paymentId, _ := InitiatePay(ctx, "user_1001", appId)
	txn := "txn_" + guid.S()
	defer cleanup(t, ctx, slotId, appId, paymentId, txn)

	if err := HandleNotify(ctx, NotifyInput{PaymentId: paymentId, TransactionId: txn, Success: false, Amount: consts.PriceCent}); err != nil {
		t.Fatalf("failed notify: %v", err)
	}
	// 订单保持待支付，支付单失败(3)
	if s := statusOf(t, appId); s != consts.StatusPending {
		t.Fatalf("order should stay 1, got %d", s)
	}
	v, _ := g.DB().Model("payment").Where("payment_id", paymentId).Value("status")
	if v.Int() != consts.PayStatusFailed {
		t.Fatalf("payment should be failed(3), got %d", v.Int())
	}

	// 失败后可重新发起支付
	paymentId2, err := InitiatePay(ctx, "user_1001", appId)
	if err != nil {
		t.Fatalf("re-initiate after failed: %v", err)
	}
	if paymentId2 == "" {
		t.Fatal("re-initiate should return paymentId")
	}
	_, _ = g.DB().Model("payment").Where("payment_id", paymentId2).Delete()
	_, _ = g.DB().Model("payment_log").Where("payment_id", paymentId2).Delete()
}
