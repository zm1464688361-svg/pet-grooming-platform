// Package pay 支付发起与回调处理（模拟支付渠道）。
//
// 约束口径（AGENTS.md §4.3）：
//   - 发起支付前置校验：本人订单 + status=1 + 未超时；30 秒 Redis 锁防重复点击；
//   - 回调幂等：同 transactionId 重复回调幂等返回成功；异号第二次支付成功 → 支付异常单；
//   - 关闭后支付成功 → 支付异常单（订单保持终态，不释放名额）；
//   - 每次回调处理写 payment_log（完整支付记录）；
//   - 订单/支付单状态更新使用 DB 条件更新（乐观锁），禁止先改后查。
package pay

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/model/biz"
)

const (
	payLockPrefix = "pay:lock:" // 发起支付锁：pay:lock:{appId}，30 秒
	notifyLockPre = "pay:cb:"   // 回调幂等锁：pay:cb:{transactionId}，5 分钟
	notifyLockTTL = 300         // 回调幂等锁秒数
	payLockTTL    = 30          // 发起支付锁秒数
)

func payLockKey(appId string) string  { return payLockPrefix + appId }
func notifyLockKey(txn string) string { return notifyLockPre + txn }

// entityApp 订单最小实体。
type entityApp struct {
	AppId  string `orm:"app_id"`
	UserId string `orm:"user_id"`
	Status int    `orm:"status"`
	Price  int64  `orm:"price"`
	SlotId string `orm:"slot_id"`
	PetId  string `orm:"pet_id"`
}

// NotifyInput 渠道回调入参。
type NotifyInput struct {
	PaymentId     string
	TransactionId string
	Success       bool
	Amount        int64
}

// InitiatePay 发起支付：校验通过后创建待支付支付单。
// 返回 paymentId；错误码：40001 非本人 / 40005 已超时 / 40008 并发点击 / 40009 状态不支持。
func InitiatePay(ctx context.Context, userId, appId string) (string, error) {
	// 1. 订单校验
	var app entityApp
	err := g.DB().Model("appointment").Where("app_id", appId).Scan(&app)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", biz.New(consts.ErrNoPermission)
		}
		return "", err
	}
	if app.UserId != userId {
		return "", biz.New(consts.ErrNoPermission)
	}
	if app.Status != consts.StatusPending {
		return "", biz.New(consts.ErrPayNotAllowed)
	}

	// 2. 严格超时校验（服务端时间）
	v, err := g.DB().Model("appointment").Where("app_id", appId).Value("lock_expire_at")
	if err != nil {
		return "", err
	}
	if v != nil && !v.Time().After(time.Now()) {
		return "", biz.New(consts.ErrOrderTimeout)
	}

	// 3. 30 秒防重复点击锁
	res, err := g.Redis().Do(ctx, "SET", payLockKey(appId), "1", "NX", "EX", payLockTTL)
	if err != nil {
		return "", err
	}
	if res.String() != "OK" {
		return "", biz.New(consts.ErrPayProcessing)
	}

	// 4. 创建/复用支付单（uk_app 唯一：失败后重新发起时重置原支付单）
	existing, err := g.DB().Model("payment").Where("app_id", appId).One()
	if err != nil {
		_, _ = g.Redis().Do(ctx, "DEL", payLockKey(appId))
		return "", err
	}
	if !existing.IsEmpty() {
		paymentId := existing["payment_id"].String()
		_, err = g.DB().Model("payment").Where("payment_id", paymentId).Data(g.Map{
			"status":         consts.PayStatusUnpaid,
			"transaction_id": "",
			"paid_at":        nil,
		}).Update()
		if err != nil {
			_, _ = g.Redis().Do(ctx, "DEL", payLockKey(appId))
			return "", err
		}
		glog.Infof(ctx, "[pay] re-initiated app=%s payment=%s", appId, paymentId)
		return paymentId, nil
	}

	paymentId := "pay_" + guid.S()
	_, err = g.DB().Model("payment").Data(g.Map{
		"payment_id": paymentId,
		"app_id":     appId,
		"amount":     app.Price,
		"status":     consts.PayStatusUnpaid,
		"created_at": time.Now(),
	}).Insert()
	if err != nil {
		_, _ = g.Redis().Do(ctx, "DEL", payLockKey(appId))
		return "", err
	}
	glog.Infof(ctx, "[pay] initiated app=%s payment=%s", appId, paymentId)
	return paymentId, nil
}

// HandleNotify 处理渠道回调（幂等）。
func HandleNotify(ctx context.Context, in NotifyInput) error {
	// 1. 同 transactionId 幂等锁：重复回调直接返回成功
	res, err := g.Redis().Do(ctx, "SET", notifyLockKey(in.TransactionId), "1", "NX", "EX", notifyLockTTL)
	if err != nil {
		return err
	}
	if res.String() != "OK" {
		glog.Infof(ctx, "[pay] duplicate transactionId=%s, idempotent success", in.TransactionId)
		return nil
	}

	// 2. 支付单校验
	var payRow struct {
		PaymentId string `orm:"payment_id"`
		AppId     string `orm:"app_id"`
		Amount    int64  `orm:"amount"`
		Status    int    `orm:"status"`
	}
	err = g.DB().Model("payment").Where("payment_id", in.PaymentId).Scan(&payRow)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return biz.New(consts.ErrPayNotAllowed)
		}
		return err
	}

	newStatus := consts.PayStatusPaid
	if !in.Success {
		newStatus = consts.PayStatusFailed
	}

	var excType int
	txErr := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		// 3. 读取支付单当前状态（条件更新前置判断）
		cur, err := tx.Model("payment").Ctx(ctx).
			Where("payment_id", in.PaymentId).
			One()
		if err != nil {
			return err
		}
		curStatus := cur["status"].Int()
		curTxn := cur["transaction_id"].String()

		// 3.1 已支付：
		//   - 同 transactionId → 幂等返回（Redis 锁兜底之外的双保险）
		//   - 异号且本次成功 → 异号重复支付，记录日志 + 支付异常单，支付单与订单不变
		if curStatus == consts.PayStatusPaid {
			if curTxn == in.TransactionId {
				return nil
			}
			if in.Success {
				if _, err := tx.Model("payment_log").Ctx(ctx).Data(g.Map{
					"log_id":         "plog_" + guid.S(),
					"app_id":         payRow.AppId,
					"transaction_id": in.TransactionId,
					"amount":         in.Amount,
					"status":         consts.PayStatusPaid,
					"created_at":     time.Now(),
				}).Insert(); err != nil {
					return err
				}
				excType = consts.ExcTypeDuplicate
			}
			return nil
		}

		// 3.2 待支付/失败 → 条件更新为本回调结果（防并发重复处理）
		affected, err := tx.Model("payment").Ctx(ctx).
			Where("payment_id", in.PaymentId).
			Where("status", curStatus).
			Data(g.Map{
				"status":         newStatus,
				"transaction_id": in.TransactionId,
				"paid_at":        time.Now(),
			}).
			Update()
		if err != nil {
			return err
		}
		if n, _ := affected.RowsAffected(); n == 0 {
			return nil // 并发竞态：已被其他回调处理，幂等返回
		}

		// 4. 支付日志（完整记录）
		if _, err := tx.Model("payment_log").Ctx(ctx).Data(g.Map{
			"log_id":         "plog_" + guid.S(),
			"app_id":         payRow.AppId,
			"transaction_id": in.TransactionId,
			"amount":         in.Amount,
			"status":         newStatus,
			"created_at":     time.Now(),
		}).Insert(); err != nil {
			return err
		}

		if !in.Success {
			return nil // 支付失败：订单保持待支付，可重新发起
		}

		// 5. 订单状态判定（条件更新，防并发）
		// 5.1 待支付 → 已确认
		r, err := tx.Model("appointment").Ctx(ctx).
			Where("app_id", payRow.AppId).
			Where("status", consts.StatusPending).
			Data(g.Map{"status": consts.StatusConfirmed, "updated_at": time.Now()}).
			Update()
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n > 0 {
			return nil // 正常确认成功
		}

		// 5.2 非待支付：查询当前状态决定异常类型
		appStatus, err := tx.Model("appointment").Ctx(ctx).Where("app_id", payRow.AppId).Value("status")
		if err != nil {
			return err
		}
		switch {
		case appStatus.Int() == consts.StatusConfirmed:
			// 订单已确认但本回调非首次成功 → 异号重复支付
			excType = consts.ExcTypeDuplicate
		default:
			// 已关闭/已取消等状态收到支付成功 → 关闭后支付异常
			excType = consts.ExcTypeClosedAfterPay
		}
		return nil
	})
	if txErr != nil {
		return txErr
	}

	// 回调处理完成后释放发起锁（失败可重新发起；成功后由状态校验拦截）
	_, _ = g.Redis().Do(ctx, "DEL", payLockKey(payRow.AppId))
	if txErr != nil {
		return txErr
	}

	// 6. 支付异常单登记（事务外，独立落库，不影响订单状态）
	if excType != 0 {
		if _, err := g.DB().Model("pay_exception").Data(g.Map{
			"exc_id":         "exc_" + guid.S(),
			"shop_id":        consts.ShopId,
			"app_id":         payRow.AppId,
			"transaction_id": in.TransactionId,
			"amount":         in.Amount,
			"exc_type":       excType,
			"status":         consts.ExcStatusPending,
			"created_at":     time.Now(),
		}).Insert(); err != nil {
			glog.Errorf(ctx, "[pay] ALARM insert exception failed: %v", err)
			return err
		}
		glog.Errorf(ctx, "[pay] ALARM app=%s payment exception type=%d txn=%s", payRow.AppId, excType, in.TransactionId)
	}
	return nil
}
