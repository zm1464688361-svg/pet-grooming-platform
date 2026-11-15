package user

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/pay"
	"pet-grooming/internal/logic/quota"
)

// newSlotAt 创建指定开始时间的测试时段。
func newSlotAt(t *testing.T, ctx context.Context, startOffset time.Duration) string {
	t.Helper()
	slotId := "slot_c_" + guid.S()
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      consts.ShopId,
		"start_time":   time.Now().Add(startOffset),
		"end_time":     time.Now().Add(startOffset + time.Hour),
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

func cleanupCancel(t *testing.T, ctx context.Context, slotId, appId string, txns ...string) {
	t.Helper()
	if appId != "" {
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("payment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("payment_log").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("refund").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("oper_log").Where("app_id", appId).Delete()
	}
	for _, txn := range txns {
		_, _ = g.Redis().Do(ctx, "DEL", "pay:cb:"+txn)
	}
	_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	_, _ = g.DB().Model("time_slot").Where("slot_id", slotId).Delete()
}

func bookAndPay(t *testing.T, ctx context.Context, slotId, petId, userId string) string {
	t.Helper()
	res, err := Book(ctx, &BookInput{UserId: userId, PetId: petId, SlotId: slotId})
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	paymentId, err := pay.InitiatePay(ctx, userId, res.AppId)
	if err != nil {
		t.Fatalf("initiate pay: %v", err)
	}
	txn := "txn_" + guid.S()
	if err := pay.HandleNotify(ctx, pay.NotifyInput{PaymentId: paymentId, TransactionId: txn, Success: true, Amount: consts.PriceCent}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	return res.AppId
}

func appField(t *testing.T, appId, field string) *g.Var {
	t.Helper()
	v, err := g.DB().Model("appointment").Where("app_id", appId).Value(field)
	if err != nil || v == nil {
		t.Fatalf("query %s: %v", field, err)
	}
	return v
}

func TestCancelPendingDirect(t *testing.T) {
	ctx := context.Background()
	slotId := newSlotAt(t, ctx, 3*time.Hour)

	res, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	appId := res.AppId
	defer cleanupCancel(t, ctx, slotId, appId)

	r, err := Cancel(ctx, "user_1001", appId, "", "不想去了")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if r.Action != CancelActionClosed {
		t.Fatalf("pending cancel should be direct closed, got %s", r.Action)
	}
	if s := appField(t, appId, "status").Int(); s != consts.StatusCancelled {
		t.Fatalf("status should be 4, got %d", s)
	}
	// 名额已释放
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity {
		t.Fatalf("quota should be released, remaining=%d", remaining)
	}
	// 无退款单（未支付）
	n, _ := g.DB().Model("refund").Where("app_id", appId).Count()
	if n != 0 {
		t.Fatalf("unpaid cancel should not create refund, got %d", n)
	}
}

func TestCancelConfirmedAutoAccepted(t *testing.T) {
	ctx := context.Background()
	slotId := newSlotAt(t, ctx, 3*time.Hour) // ≥2h
	appId := bookAndPay(t, ctx, slotId, "pet_2002", "user_1001")
	defer cleanupCancel(t, ctx, slotId, appId)

	r, err := Cancel(ctx, "user_1001", appId, "", "临时有事")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if r.Action != CancelActionAccepted {
		t.Fatalf("≥2h cancel should auto accept, got %s", r.Action)
	}
	if s := appField(t, appId, "status").Int(); s != consts.StatusCancelled {
		t.Fatalf("status should be 4, got %d", s)
	}
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity {
		t.Fatalf("quota should be released, remaining=%d", remaining)
	}
	// 已支付 → 生成待退款单（type=1 取消退款）
	v, _ := g.DB().Model("refund").Where("app_id", appId).One()
	if v.IsEmpty() {
		t.Fatal("paid cancel should create refund row")
	}
	if v["refund_type"].Int() != consts.RefundTypeCancel || v["status"].Int() != consts.RefundStatusPending || v["amount"].Int() != consts.PriceCent {
		t.Fatalf("unexpected refund row: %v", v)
	}
}

func TestCancelWithin2hGoesAudit(t *testing.T) {
	ctx := context.Background()
	slotId := newSlotAt(t, ctx, 1*time.Hour) // 不足 2h
	appId := bookAndPay(t, ctx, slotId, "pet_2003", "user_1002")
	defer cleanupCancel(t, ctx, slotId, appId)

	// 未填原因 → 40010
	if _, err := Cancel(ctx, "user_1002", appId, "", ""); err == nil || err.Error() != "40010" {
		t.Fatalf("missing reason should fail 40010, got %v", err)
	}

	r, err := Cancel(ctx, "user_1002", appId, "", "宠物生病")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if r.Action != CancelActionAudit {
		t.Fatalf("<2h cancel should go audit, got %s", r.Action)
	}
	if s := appField(t, appId, "status").Int(); s != consts.StatusCancelAudit {
		t.Fatalf("status should be 3, got %d", s)
	}
	if appField(t, appId, "cancel_apply_at").IsNil() {
		t.Fatal("cancel_apply_at should be set")
	}
	// 名额冻结（保持占用）
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity-1 {
		t.Fatalf("quota should be frozen, remaining=%d", remaining)
	}
}

func TestCancelAuditPendingRejected(t *testing.T) {
	ctx := context.Background()
	slotId := newSlotAt(t, ctx, 1*time.Hour)
	appId := bookAndPay(t, ctx, slotId, "pet_2005", "user_1003")
	defer cleanupCancel(t, ctx, slotId, appId)

	if _, err := Cancel(ctx, "user_1003", appId, "", "宠物生病"); err != nil {
		t.Fatalf("first cancel: %v", err)
	}
	// 状态 3 重复提交 → 40011
	if _, err := Cancel(ctx, "user_1003", appId, "", "宠物生病"); err == nil || err.Error() != "40011" {
		t.Fatalf("repeat cancel should fail 40011, got %v", err)
	}
}

func TestCancelFinalStateRejected(t *testing.T) {
	ctx := context.Background()
	slotId := newSlotAt(t, ctx, 3*time.Hour)

	res, _ := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	appId := res.AppId
	defer cleanupCancel(t, ctx, slotId, appId)

	if _, err := Cancel(ctx, "user_1001", appId, "", "不想去了"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// 已取消(4) 再取消 → 40006
	if _, err := Cancel(ctx, "user_1001", appId, "", "不想去了"); err == nil || err.Error() != "40006" {
		t.Fatalf("final state cancel should fail 40006, got %v", err)
	}
}

func TestCancelNotOwner(t *testing.T) {
	ctx := context.Background()
	slotId := newSlotAt(t, ctx, 3*time.Hour)

	res, _ := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	defer cleanupCancel(t, ctx, slotId, res.AppId)

	if _, err := Cancel(ctx, "user_1002", res.AppId, "", "不想去了"); err == nil || err.Error() != "40001" {
		t.Fatalf("not owner should fail 40001, got %v", err)
	}
}
