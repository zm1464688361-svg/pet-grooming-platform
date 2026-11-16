package shop

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
)

// ledgerApp 创建台账日期的预约（slot start = 明天，date=明天 YYYY-MM-DD）。
func ledgerApp(t *testing.T, ctx context.Context, slotId, petId, userId string, status int) string {
	t.Helper()
	appId := "app_l_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":     appId,
		"user_id":    userId,
		"pet_id":     petId,
		"slot_id":    slotId,
		"shop_id":    consts.ShopId,
		"item_id":    consts.ItemId,
		"price":      consts.PriceCent,
		"status":     status,
		"created_at": time.Now(),
		"updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert ledger app: %v", err)
	}
	return appId
}

// markPaid 插入成功支付日志（已收款判定来源）。
func markPaid(t *testing.T, appId string) {
	t.Helper()
	_, err := g.DB().Model("payment_log").Data(g.Map{
		"log_id":         "plog_l_" + guid.S(),
		"app_id":         appId,
		"transaction_id": "txn_l_" + guid.S(),
		"amount":         consts.PriceCent,
		"status":         consts.PayStatusPaid,
		"created_at":     time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert payment_log: %v", err)
	}
}

func TestDailyLedger(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	// 用远未来日期隔离测试数据，避免被演示种子数据污染
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(365*24*time.Hour), time.Now().Add(365*24*time.Hour+time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)
	date := time.Now().Add(365 * 24 * time.Hour).Format("2006-01-02")

	// 五类计数样本
	aValid := ledgerApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusConfirmed)   // 有效(2)
	aAudit := ledgerApp(t, ctx, slotId, "pet_2003", "user_1002", consts.StatusCancelAudit) // 有效(3)+退款待处理
	aDone := ledgerApp(t, ctx, slotId, "pet_2005", "user_1003", consts.StatusFinished)     // 已完成(7)
	aCancel := ledgerApp(t, ctx, slotId, "pet_2007", "user_1004", consts.StatusCancelled)  // 取消(4)
	aNoShow := ledgerApp(t, ctx, slotId, "pet_2009", "user_1005", consts.StatusNoShow)     // 未到店(8)
	defer func() {
		_, _ = g.DB().Model("appointment").WhereIn("app_id", g.Slice{aValid, aAudit, aDone, aCancel, aNoShow}).Delete()
		_, _ = g.DB().Model("payment_log").WhereIn("app_id", g.Slice{aValid, aAudit, aDone, aCancel, aNoShow}).Delete()
		_, _ = g.DB().Model("refund").WhereIn("app_id", g.Slice{aCancel, aDone}).Delete()
		_, _ = g.DB().Model("oper_log").WhereIn("app_id", g.Slice{aCancel, aDone}).Delete()
	}()

	// 全部已收款（按预约维度只计一笔）
	for _, id := range []string{aValid, aAudit, aDone, aCancel, aNoShow} {
		markPaid(t, id)
	}
	// 取消单：退款成功 9900（type=1）
	_, _ = g.DB().Model("refund").Data(g.Map{
		"refund_id":   "ref_l_" + guid.S(),
		"app_id":      aCancel,
		"refund_type": consts.RefundTypeCancel,
		"amount":      consts.PriceCent,
		"status":      consts.RefundStatusSuccess,
		"refunded_at": time.Now(),
		"created_at":  time.Now(),
	}).Insert()
	// 已完成单：存在一笔待退款（type=1，退款中）→ 未退款
	_, _ = g.DB().Model("refund").Data(g.Map{
		"refund_id":   "ref_l_" + guid.S(),
		"app_id":      aDone,
		"refund_type": consts.RefundTypeCancel,
		"amount":      consts.PriceCent,
		"status":      consts.RefundStatusPending,
		"created_at":  time.Now(),
	}).Insert()

	sum, slots, err := DailyLedger(ctx, date)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	// 五类计数
	if sum.ValidCount != 2 {
		t.Fatalf("valid should be 2, got %d", sum.ValidCount)
	}
	if sum.FinishedCount != 1 {
		t.Fatalf("finished should be 1, got %d", sum.FinishedCount)
	}
	if sum.CancelledCount != 1 {
		t.Fatalf("cancelled should be 1, got %d", sum.CancelledCount)
	}
	if sum.NoShowCount != 1 {
		t.Fatalf("noshow should be 1, got %d", sum.NoShowCount)
	}
	// 退款待处理 = status3(1) + 退款中(1) = 2
	if sum.RefundPendingCount != 2 {
		t.Fatalf("refundPending should be 2, got %d", sum.RefundPendingCount)
	}

	// 四项金额独立
	if sum.CollectedAmount != 5*consts.PriceCent {
		t.Fatalf("collected should be %d, got %d", 5*consts.PriceCent, sum.CollectedAmount)
	}
	if sum.RevenueAmount != consts.PriceCent { // 仅 status=7 计收入
		t.Fatalf("revenue should be %d, got %d", consts.PriceCent, sum.RevenueAmount)
	}
	if sum.RefundedAmount != consts.PriceCent {
		t.Fatalf("refunded should be %d, got %d", consts.PriceCent, sum.RefundedAmount)
	}
	if sum.UnrefundedAmount != consts.PriceCent {
		t.Fatalf("unrefunded should be %d, got %d", consts.PriceCent, sum.UnrefundedAmount)
	}

	// 汇总与下钻一致（明细为准）
	var agg LedgerSummary
	for _, s := range slots {
		agg.ValidCount += s.ValidCount
		agg.FinishedCount += s.FinishedCount
		agg.CancelledCount += s.CancelledCount
		agg.NoShowCount += s.NoShowCount
		agg.RefundPendingCount += s.RefundPendingCount
		agg.CollectedAmount += s.CollectedAmount
		agg.RevenueAmount += s.RevenueAmount
		agg.RefundedAmount += s.RefundedAmount
		agg.UnrefundedAmount += s.UnrefundedAmount
	}
	if agg != *sum {
		t.Fatalf("summary != slots detail: %+v vs %+v", *sum, agg)
	}

	// 角色越权：前台 → 40001
	if _, _, err := DailyLedger(staffCtx(consts.RoleFront, consts.ShopId), date); err == nil || err.Error() != "40001" {
		t.Fatalf("front ledger should fail 40001, got %v", err)
	}
}

func TestLedgerExcludesOtherDays(t *testing.T) {
	ctx := staffCtx(consts.RoleManager, consts.ShopId)
	// 时段在查询日期前一天（远未来日期隔离，避免演示数据污染）
	slotId := newSlotRow(t, ctx, consts.ShopId, time.Now().Add(364*24*time.Hour), time.Now().Add(364*24*time.Hour+time.Hour), consts.SlotActive)
	defer cleanupSlot(t, ctx, slotId)

	appId := ledgerApp(t, ctx, slotId, "pet_2001", "user_1001", consts.StatusFinished)
	defer func() {
		_, _ = g.DB().Model("appointment").Where("app_id", appId).Delete()
		_, _ = g.DB().Model("payment_log").Where("app_id", appId).Delete()
	}()
	markPaid(t, appId)

	date := time.Now().Add(365 * 24 * time.Hour).Format("2006-01-02")
	sum, _, err := DailyLedger(ctx, date)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if sum.RevenueAmount != 0 || sum.CollectedAmount != 0 {
		t.Fatalf("other day's data must be excluded, collected=%d revenue=%d", sum.CollectedAmount, sum.RevenueAmount)
	}
}
