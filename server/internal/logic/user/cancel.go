// Package user C 端取消申请。
//
// 约束口径（AGENTS.md §4.2）：
//   - 待支付(1)：主动取消 1→4，事务提交后立即释放本单名额，无退款单；
//   - 已确认(2) 且距开始 ≥2h（秒级，恰好 2h 按 ≥2h）：自动受理 2→4，释放名额，
//     已支付则生成全额待退款单（type=1 取消退款）；
//   - 已确认(2) 且不足 2h：转客服审核 2→3（冻结名额，不可核销），原因必填（40010）；
//   - 状态 3 重复提交 → 40011；终态(4~9) → 40006；
//   - 幂等键 SETNX：重复提交 → 40011。
package user

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
	"pet-grooming/internal/model/biz"
)

// 取消动作结果
const (
	CancelActionClosed   = "closed"   // 直接取消成功（含待支付主动取消）
	CancelActionAccepted = "accepted" // ≥2h 自动受理
	CancelActionAudit    = "audit"    // 不足 2h 转客服审核
)

// CancelResult 取消结果。
type CancelResult struct {
	AppId   string `json:"appId"`
	Status  int    `json:"status"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

// cancelIdemKey 取消幂等键。
func cancelIdemKey(userId, appId string) string {
	return "cancel:idem:" + userId + ":" + appId
}

// entityAppCancel 取消校验用订单实体。
type entityAppCancel struct {
	AppId  string `orm:"app_id"`
	UserId string `orm:"user_id"`
	Status int    `orm:"status"`
	Price  int64  `orm:"price"`
	SlotId string `orm:"slot_id"`
	PetId  string `orm:"pet_id"`
}

// Cancel 发起取消申请。
func Cancel(ctx context.Context, userId, appId, idemKey, reason string) (*CancelResult, error) {
	// 1. 订单归属校验（40001）
	var app entityAppCancel
	err := g.DB().Model("appointment").Where("app_id", appId).Scan(&app)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if app.AppId == "" || app.UserId != userId {
		return nil, biz.New(consts.ErrNoPermission)
	}

	// 2. 状态分流（终态/审核中直接拒绝，不占用幂等键）
	var (
		handler     func() error
		result      *CancelResult
		needRelease bool
	)
	switch app.Status {
	case consts.StatusPending:
		handler = func() error { return cancelPending(ctx, &app) }
		result = &CancelResult{AppId: appId, Status: consts.StatusCancelled, Action: CancelActionClosed, Message: "取消成功，名额已释放"}
		needRelease = true
	case consts.StatusConfirmed:
		startAt, err := slotStartTime(ctx, app.SlotId)
		if err != nil {
			return nil, err
		}
		if startAt.Sub(time.Now()) >= consts.CancelWindowHours*time.Hour {
			// ≥2h：自动受理 2→4，释放名额；已支付生成全额待退款单
			handler = func() error { return cancelConfirmedAccepted(ctx, &app) }
			result = &CancelResult{AppId: appId, Status: consts.StatusCancelled, Action: CancelActionAccepted, Message: "取消成功，名额已释放"}
			needRelease = true
		} else {
			// 不足 2h：原因必填（40010），转审核 2→3（冻结名额）
			if reason == "" {
				return nil, biz.New(consts.ErrCancelReasonNeed)
			}
			handler = func() error { return cancelConfirmedAudit(ctx, &app, reason) }
			result = &CancelResult{AppId: appId, Status: consts.StatusCancelAudit, Action: CancelActionAudit, Message: consts.MsgCancelAudit}
		}
	case consts.StatusCancelAudit:
		// 状态 3 不可重复提交取消（40011）
		return nil, biz.New(consts.ErrCancelDuplicated)
	default:
		// 终态 4~9 不可取消（40006）
		return nil, biz.New(consts.ErrCancelNotAllowed)
	}

	// 3. 幂等：实际流转前 SETNX（校验失败不占用键），重复提交 → 40011
	res, err := g.Redis().Do(ctx, "SET", cancelIdemKey(userId, appId), "1", "NX", "EX", 600)
	if err != nil {
		return nil, err
	}
	if res.String() != "OK" {
		return nil, biz.New(consts.ErrCancelDuplicated)
	}

	// 4. 执行流转；业务拒绝则回滚幂等键
	if err := handler(); err != nil {
		if _, ok := err.(*biz.Error); ok {
			_, _ = g.Redis().Do(ctx, "DEL", cancelIdemKey(userId, appId))
		}
		return nil, err
	}

	// 5. 事务已提交：释放名额
	if needRelease {
		releaseAfterCommit(ctx, app.SlotId, app.PetId)
	}
	return result, nil
}

// releaseAfterCommit 事务提交成功后释放本单名额（唯一释放入口 quota.Release）。
func releaseAfterCommit(ctx context.Context, slotId, petId string) {
	if err := quota.Release(ctx, slotId, petId); err != nil {
		glog.Errorf(ctx, "[cancel] ALARM release failed slot=%s pet=%s err=%v", slotId, petId, err)
		if rErr := quota.RebuildQuota(ctx, slotId); rErr != nil {
			glog.Errorf(ctx, "[cancel] ALARM rebuild failed: %v", rErr)
		}
	}
}

// slotStartTime 查询时段开始时间（服务端口径）。
func slotStartTime(ctx context.Context, slotId string) (time.Time, error) {
	v, err := g.DB().Model("time_slot").Where("slot_id", slotId).Value("start_time")
	if err != nil {
		return time.Time{}, err
	}
	if v == nil {
		return time.Time{}, biz.New(consts.ErrSlotUnavailable)
	}
	return v.Time(), nil
}

// cancelPending 待支付直接取消（1→4）。
func cancelPending(ctx context.Context, app *entityAppCancel) error {
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		r, err := tx.Model("appointment").Ctx(ctx).
			Where("app_id", app.AppId).
			Where("status", consts.StatusPending).
			Data(g.Map{"status": consts.StatusCancelled, "updated_at": time.Now()}).
			Update()
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return biz.New(consts.ErrCancelNotAllowed) // 并发竞态：状态已被变更
		}
		return writeCancelOperLog(ctx, tx, app, consts.StatusPending, consts.StatusCancelled, "待支付主动取消")
	})
}

// cancelConfirmedAccepted 已确认 ≥2h 自动受理（2→4），已支付生成退款单。
func cancelConfirmedAccepted(ctx context.Context, app *entityAppCancel) error {
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		r, err := tx.Model("appointment").Ctx(ctx).
			Where("app_id", app.AppId).
			Where("status", consts.StatusConfirmed).
			Data(g.Map{"status": consts.StatusCancelled, "updated_at": time.Now()}).
			Update()
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return biz.New(consts.ErrCancelNotAllowed)
		}

		// 已支付（存在支付成功记录）→ 全额待退款单（type=1）
		paid, err := tx.Model("payment").Ctx(ctx).
			Where("app_id", app.AppId).
			Where("status", consts.PayStatusPaid).
			Count()
		if err != nil {
			return err
		}
		if paid > 0 {
			if _, err := tx.Model("refund").Ctx(ctx).Data(g.Map{
				"refund_id":   "ref_" + guid.S(),
				"app_id":      app.AppId,
				"refund_type": consts.RefundTypeCancel,
				"amount":      app.Price,
				"status":      consts.RefundStatusPending,
				"created_at":  time.Now(),
			}).Insert(); err != nil {
				return err
			}
		}
		return writeCancelOperLog(ctx, tx, app, consts.StatusConfirmed, consts.StatusCancelled, "≥2小时自助取消，自动受理")
	})
}

// cancelConfirmedAudit 不足 2h 转客服审核（2→3），冻结名额。
func cancelConfirmedAudit(ctx context.Context, app *entityAppCancel, reason string) error {
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		r, err := tx.Model("appointment").Ctx(ctx).
			Where("app_id", app.AppId).
			Where("status", consts.StatusConfirmed).
			Data(g.Map{
				"status":          consts.StatusCancelAudit,
				"cancel_apply_at": time.Now(),
				"cancel_reason":   reason,
				"updated_at":      time.Now(),
			}).
			Update()
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return biz.New(consts.ErrCancelDuplicated)
		}
		return writeCancelOperLog(ctx, tx, app, consts.StatusConfirmed, consts.StatusCancelAudit, "不足2小时，转客服审核："+reason)
	})
}

// writeCancelOperLog 取消动作日志。
func writeCancelOperLog(ctx context.Context, tx gdb.TX, app *entityAppCancel, from, to int, remark string) error {
	_, err := tx.Model("oper_log").Ctx(ctx).Data(g.Map{
		"log_id":      "log_" + guid.S(),
		"shop_id":     consts.ShopId,
		"staff_id":    "",
		"app_id":      app.AppId,
		"action":      "user_cancel",
		"from_status": from,
		"to_status":   to,
		"remark":      remark,
		"created_at":  time.Now(),
	}).Insert()
	return err
}
