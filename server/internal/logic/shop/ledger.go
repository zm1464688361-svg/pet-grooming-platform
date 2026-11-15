// Package shop 日清台账（店长）。
//
// 约束口径（agentrules-B端 §12）：
//   - 按日期查询，按 slot_id 分组，支持下钻明细；
//   - 五类计数：有效预约（2、3）、已完成（7）、取消（4）、未到店（8）、退款待处理（3 待审 + 退款中/失败）；
//   - 四项金额独立：已收款（支付成功，按预约维度只计一笔）、已完成服务收入（仅 7）、
//     已退款（退款成功含取消/重复支付/异常三类）、未退款（退款中 + 失败）；
//   - 重复支付多收款项只出现在已退款/未退款，不冲减服务收入，也不计入已收款；
//   - 聚合收敛到一个方法，以明细为准修聚合（汇总 = Σ下钻），禁止手工改汇总值；
//   - 台账业务日期 = 时段开始日期（docs/plans/assumptions.md 假设 4）。
package shop

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/biz"
)

// LedgerSummary 台账五类计数 + 四项金额。
type LedgerSummary struct {
	ValidCount         int   `json:"validCount"`         // 有效预约（2、3）
	FinishedCount      int   `json:"finishedCount"`      // 已完成（7）
	CancelledCount     int   `json:"cancelledCount"`     // 取消（4）
	NoShowCount        int   `json:"noShowCount"`        // 未到店（8）
	RefundPendingCount int   `json:"refundPendingCount"` // 退款待处理（3 待审 + 退款中/失败）
	CollectedAmount    int64 `json:"collectedAmount"`    // 已收款（按预约维度只计一笔）
	RevenueAmount      int64 `json:"revenueAmount"`      // 已完成服务收入（仅 7）
	RefundedAmount     int64 `json:"refundedAmount"`     // 已退款（退款成功）
	UnrefundedAmount   int64 `json:"unrefundedAmount"`   // 未退款（退款中+失败）
}

// LedgerSlotRow 按时段下钻明细行（与汇总同口径）。
type LedgerSlotRow struct {
	SlotId    string `json:"slotId"`
	StartTime string `json:"startTime"`
	LedgerSummary
}

type ledgerAppRow struct {
	AppId     string `orm:"app_id"`
	SlotId    string `orm:"slot_id"`
	Status    int    `orm:"status"`
	Price     int64  `orm:"price"`
	StartTime string `orm:"start_time"`
}

// DailyLedger 日清台账：入参 shop_id + date（YYYY-MM-DD），返回汇总与按时段下钻明细。
func DailyLedger(ctx context.Context, date string) (*LedgerSummary, []*LedgerSlotRow, error) {
	staff := auth.StaffFrom(ctx)
	// 台账为店长专属（前台/服务/客服越权 → 40001）
	if staff == nil || staff.Role != consts.RoleManager {
		return nil, nil, biz.New(consts.ErrNoPermission)
	}
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	// 1. 当日订单明细（时段开始日期 = 业务日期）
	var apps []*ledgerAppRow
	err := g.DB().Model("appointment a").
		LeftJoin("time_slot s", "s.slot_id=a.slot_id").
		Fields("a.app_id,a.slot_id,a.status,a.price,s.start_time").
		Where("a.shop_id", staff.ShopId).
		WhereLike("s.start_time", date+"%").
		Scan(&apps)
	if err != nil {
		return nil, nil, err
	}

	// 本日订单 id 集合
	appIds := make([]string, 0, len(apps))
	for _, a := range apps {
		appIds = append(appIds, a.AppId)
	}

	// 2. 已收款 app 集合（payment_log 成功，按预约维度只计一笔）
	paidSet := map[string]bool{}
	if len(appIds) > 0 {
		paidRows, err := g.DB().Model("payment_log").
			WhereIn("app_id", appIds).
			Where("status", consts.PayStatusPaid).
			Fields("DISTINCT app_id").
			All()
		if err != nil {
			return nil, nil, err
		}
		for _, r := range paidRows {
			paidSet[r["app_id"].String()] = true
		}
	}

	// 3. 退款单（含取消/重复支付/异常三类）
	type refundRow struct {
		AppId  string `orm:"app_id"`
		Status int    `orm:"status"`
		Amount int64  `orm:"amount"`
	}
	var refunds []*refundRow
	if len(appIds) > 0 {
		err = g.DB().Model("refund").
			Fields("app_id,status,amount").
			WhereIn("app_id", appIds).
			Scan(&refunds)
		if err != nil {
			return nil, nil, err
		}
	}

	// 4. 以明细为准聚合（按时段分组，汇总 = Σ下钻）
	slotAgg := map[string]*LedgerSlotRow{}
	var summary LedgerSummary
	add := func(slotId, startTime string, fn func(row *LedgerSummary)) {
		row, ok := slotAgg[slotId]
		if !ok {
			row = &LedgerSlotRow{SlotId: slotId, StartTime: startTime}
			slotAgg[slotId] = row
		}
		fn(&row.LedgerSummary)
	}

	for _, a := range apps {
		startTime := ""
		switch a.Status {
		case consts.StatusConfirmed, consts.StatusCancelAudit:
			add(a.SlotId, a.StartTime, func(s *LedgerSummary) { s.ValidCount++ })
		case consts.StatusFinished:
			add(a.SlotId, a.StartTime, func(s *LedgerSummary) { s.FinishedCount++ })
		case consts.StatusCancelled:
			add(a.SlotId, a.StartTime, func(s *LedgerSummary) { s.CancelledCount++ })
		case consts.StatusNoShow:
			add(a.SlotId, a.StartTime, func(s *LedgerSummary) { s.NoShowCount++ })
		}
		_ = startTime

		// 已收款：支付成功过的订单，按预约维度只计一笔
		if paidSet[a.AppId] {
			add(a.SlotId, a.StartTime, func(s *LedgerSummary) { s.CollectedAmount += a.Price })
		}
		// 已完成服务收入：仅 status=7
		if a.Status == consts.StatusFinished {
			add(a.SlotId, a.StartTime, func(s *LedgerSummary) { s.RevenueAmount += a.Price })
		}
	}

	// 5. 退款待处理：status=3 待审 + 退款中/失败
	for _, a := range apps {
		if a.Status == consts.StatusCancelAudit {
			add(a.SlotId, a.StartTime, func(s *LedgerSummary) { s.RefundPendingCount++ })
		}
	}
	for _, rf := range refunds {
		if rf.Status == consts.RefundStatusSuccess {
			add(appSlotOf(apps, rf.AppId), "", func(s *LedgerSummary) { s.RefundedAmount += rf.Amount })
		} else if rf.Status == consts.RefundStatusPending || rf.Status == consts.RefundStatusFailed {
			add(appSlotOf(apps, rf.AppId), "", func(s *LedgerSummary) { s.UnrefundedAmount += rf.Amount })
			add(appSlotOf(apps, rf.AppId), "", func(s *LedgerSummary) { s.RefundPendingCount++ })
		}
	}

	// 6. 汇总 = Σ下钻（明细为准）
	rows := make([]*LedgerSlotRow, 0, len(slotAgg))
	for _, row := range slotAgg {
		summary.ValidCount += row.ValidCount
		summary.FinishedCount += row.FinishedCount
		summary.CancelledCount += row.CancelledCount
		summary.NoShowCount += row.NoShowCount
		summary.RefundPendingCount += row.RefundPendingCount
		summary.CollectedAmount += row.CollectedAmount
		summary.RevenueAmount += row.RevenueAmount
		summary.RefundedAmount += row.RefundedAmount
		summary.UnrefundedAmount += row.UnrefundedAmount
		rows = append(rows, row)
	}
	// 按时段开始时间排序
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].StartTime < rows[i].StartTime {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	return &summary, rows, nil
}

// appSlotOf 订单所属时段（退款单归属）。
func appSlotOf(apps []*ledgerAppRow, appId string) string {
	for _, a := range apps {
		if a.AppId == appId {
			return a.SlotId
		}
	}
	return ""
}
