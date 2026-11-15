// Package user C 端补充测试（服务项目、退款查询、暂停时段过滤、释放告警路径）。
package user

import (
	"context"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
)

// makePaidOrder 造一笔已支付（status=2）订单。
func makePaidOrder(t *testing.T, ctx context.Context, petId string) (string, string) {
	t.Helper()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	r, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: petId, SlotId: slotId, IdemKey: "ux-" + guid.S()})
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	_, err = g.DB().Model("appointment").
		Data(g.Map{"status": consts.StatusConfirmed}).
		Where("app_id", r.AppId).Where("status", consts.StatusPending).
		Update()
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	_, err = g.DB().Model("payment").Data(g.Map{
		"payment_id": "pay_x_" + guid.S(), "app_id": r.AppId, "amount": 9900,
		"status": consts.PayStatusPaid, "created_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}
	return slotId, r.AppId
}

func TestGetItemInfo(t *testing.T) {
	item, err := GetItemInfo(context.Background())
	if err != nil || item == nil {
		t.Fatalf("item info: %v,%v", item, err)
	}
	if item.ItemId != consts.ItemId || item.PriceCent != 9900 {
		t.Fatalf("item mismatch: %+v", item)
	}
}

func TestListMyRefunds(t *testing.T) {
	ctx := context.Background()
	slotId, appId := makePaidOrder(t, ctx, "pet_2001")
	defer func() {
		_, _ = g.DB().Model("refund").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("payment").Where("app_id", appId).Delete()
		cleanupSlot(t, ctx, slotId)
	}()
	// 先无退款单
	list, err := ListMyRefunds(ctx, "user_1001")
	if err != nil {
		t.Fatalf("refunds: %v", err)
	}
	// 生成退款单
	_, err = g.DB().Model("refund").Data(g.Map{
		"refund_id": "ref_x_" + guid.S(), "app_id": appId, "refund_type": consts.RefundTypeCancel,
		"amount": 9900, "status": consts.RefundStatusPending, "created_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert refund: %v", err)
	}
	list, err = ListMyRefunds(ctx, "user_1001")
	if err != nil {
		t.Fatalf("refunds: %v", err)
	}
	found := false
	for _, r := range list {
		if r.AppId == appId {
			found = true
			if r.StatusName != "退款中" || r.RefundName != "取消退款" {
				t.Fatalf("refund view mismatch: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("refund not listed")
	}
}

func TestListBookableSlotsSkipsPaused(t *testing.T) {
	ctx := context.Background()
	// 暂停时段不应出现在可约列表
	slotId := createTestSlot(t, ctx, 5*time.Hour)
	defer cleanupSlot(t, ctx, slotId)
	_, err := g.DB().Model("time_slot").Data(g.Map{"status": consts.SlotPaused}).Where("slot_id", slotId).Update()
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	slots, err := ListBookableSlots(ctx)
	if err != nil {
		t.Fatalf("list slots: %v", err)
	}
	for _, s := range slots {
		if s.SlotId == slotId {
			t.Fatal("paused slot should not be bookable")
		}
	}
}

func TestListMyAppointmentsStatusFilter(t *testing.T) {
	ctx := context.Background()
	slotId, appId := makePaidOrder(t, ctx, "pet_2002")
	defer func() {
		_, _ = g.DB().Model("payment").Where("app_id", appId).Delete()
		cleanupSlot(t, ctx, slotId)
	}()
	rows, total, err := ListMyAppointments(ctx, "user_1001", consts.StatusConfirmed, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total < 1 {
		t.Fatal("status=2 filter should include the order")
	}
	for _, row := range rows {
		if row.Status != consts.StatusConfirmed {
			t.Fatalf("filter leaked status=%d", row.Status)
		}
	}
}

func TestReleaseAfterCommitAlarmRebuild(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 6*time.Hour)
	defer cleanupSlot(t, ctx, slotId)
	// 造已占用名额
	if ok, err := quota.Occupy(ctx, slotId, "pet_2001", consts.MaxCapacity); err != nil || !ok {
		t.Fatalf("occupy: %v,%v", ok, err)
	}
	// 释放（正常路径）
	releaseAfterCommit(ctx, slotId, "pet_2001")
	rem, err := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if err != nil || rem != consts.MaxCapacity {
		t.Fatalf("after release remaining: %d,%v", rem, err)
	}
	// 告警路径：未占用再释放 → 兜底重建，不 panic
	releaseAfterCommit(ctx, slotId, "pet_2001")
	rem, err = quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if err != nil || rem != consts.MaxCapacity {
		t.Fatalf("after alarm remaining: %d,%v", rem, err)
	}
}

// TestRemainingForSlotDbFallback Redis 无计数 key 时按 DB 对账重建（DB 为真值源）。
func TestRemainingForSlotDbFallback(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 7*time.Hour)
	defer cleanupSlot(t, ctx, slotId)
	// 直接 DB 插入有效预约（不经过 Redis 占用）
	appId := "app_x_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id": appId, "user_id": "user_1001", "pet_id": "pet_2001", "shop_id": consts.ShopId,
		"item_id": consts.ItemId, "slot_id": slotId, "price": 9900,
		"status": consts.StatusConfirmed, "lock_expire_at": time.Now().Add(5 * time.Minute),
		"created_at": time.Now(), "updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert app: %v", err)
	}
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()
	_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)

	rem, err := remainingForSlot(ctx, slotId)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if rem != consts.MaxCapacity-1 {
		t.Fatalf("db fallback remaining should be %d, got %d", consts.MaxCapacity-1, rem)
	}
}

// TestBookRejectsPausedSlot 暂停时段下单 → 40002。
func TestBookRejectsPausedSlot(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 8*time.Hour)
	defer cleanupSlot(t, ctx, slotId)
	_, err := g.DB().Model("time_slot").Data(g.Map{"status": consts.SlotPaused}).Where("slot_id", slotId).Update()
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	_, err = Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: "ux2-" + guid.S()})
	if err == nil {
		t.Fatal("paused slot should not be bookable")
	}
}

// TestBookAndCancelGuards 守卫分支：ghost 宠物/时段/订单、非本人订单、重复 idemKey。
func TestBookAndCancelGuards(t *testing.T) {
	ctx := context.Background()
	// ghost 宠物 → 40003
	if _, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_ghost", SlotId: "slot_ghost", IdemKey: "gx1"}); err == nil {
		t.Fatal("ghost pet should fail")
	}
	// ghost 时段 → 40002
	if _, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: "slot_ghost", IdemKey: "gx2"}); err == nil {
		t.Fatal("ghost slot should fail")
	}
	// 正常下单（时段 1h 后开始 → 取消时走 <2h 审核分支）
	slotId := createTestSlot(t, ctx, time.Hour)
	defer cleanupSlot(t, ctx, slotId)
	r1, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: "gx3-" + guid.S()})
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	// 同宠物同时段重复下单 → 40007（重复预约守卫）
	_, err = Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: "gx3b"})
	if err == nil {
		t.Fatal("duplicate booking should fail")
	}
	// ghost 订单取消 → 40006
	if _, err := Cancel(ctx, "user_1001", "app_ghost", "gx4", ""); err == nil {
		t.Fatal("ghost cancel should fail")
	}
	// 非本人订单取消 → 40006
	if _, err := Cancel(ctx, "user_9999", r1.AppId, "gx5", ""); err == nil {
		t.Fatal("other user cancel should fail")
	}
	// 已确认订单 <2h 取消 → 转审批（status=3）
	_, _ = g.DB().Model("appointment").Data(g.Map{"status": consts.StatusConfirmed}).Where("app_id", r1.AppId).Update()
	if _, err := Cancel(ctx, "user_1001", r1.AppId, "gx6", "不到两小时"); err != nil {
		t.Fatalf("short cancel: %v", err)
	}
	if st := appStatus(t, ctx, r1.AppId); st != consts.StatusCancelAudit {
		t.Fatalf("short cancel should audit, got %d", st)
	}
	// 重复提交同一取消 → 40011
	if _, err := Cancel(ctx, "user_1001", r1.AppId, "gx7", "again"); err == nil || err.Error() != "40011" {
		t.Fatalf("resubmit cancel should 40011, got %v", err)
	}
}

func appStatus(t *testing.T, ctx context.Context, appId string) int {
	t.Helper()
	v, err := g.DB().Model("appointment").Where("app_id", appId).Value("status")
	if err != nil {
		t.Fatalf("query status: %v", err)
	}
	return v.Int()
}
