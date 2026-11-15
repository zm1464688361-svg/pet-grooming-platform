// Package timer 定时任务：待支付订单超时关闭。
//
// 约束口径（AGENTS.md §4.2）：
//   - 严格按 lock_expire_at 判定，毫秒不提前；
//   - 超时关闭仅释放本单名额（quota.Release）；
//   - 关闭前检查支付日志：已有成功支付（回调竞态窗口）则不关闭并告警；
//   - 关闭动作写入 oper_log（系统动作，staff 为空）。
package timer

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcron"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
)

type releaseTask struct{ slotId, petId string }

type entityApp struct {
	AppId  string `orm:"app_id"`
	ShopId string `orm:"shop_id"`
	SlotId string `orm:"slot_id"`
	PetId  string `orm:"pet_id"`
}

// Start 启动超时扫描定时任务（每 30 秒）。
func Start(ctx context.Context) {
	registerTimeoutJob(ctx, "@every 30s", "timeout_close")
}

// registerTimeoutJob 注册扫描任务（间隔与任务名参数化，测试可用更短间隔）。
func registerTimeoutJob(ctx context.Context, interval, name string) {
	_, err := gcron.Add(ctx, interval, func(jobCtx context.Context) {
		closed, err := CloseTimeoutOnce(jobCtx)
		if err != nil {
			glog.Errorf(ctx, "[timer] timeout close scan failed: %v", err)
			return
		}
		if closed > 0 {
			glog.Infof(ctx, "[timer] timeout closed %d orders", closed)
		}
	}, name)
	if err != nil {
		glog.Errorf(ctx, "[timer] register cron failed: %v", err)
	}
}

// CloseTimeoutOnce 单次扫描关闭超时订单，返回关闭数量。
func CloseTimeoutOnce(ctx context.Context) (int, error) {
	closed := 0
	var toRelease []releaseTask

	txErr := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		// 锁定待关闭订单行（严格 lock_expire_at < now）
		var apps []*entityApp
		err := tx.Model("appointment").Ctx(ctx).
			Where("status", consts.StatusPending).
			WhereLT("lock_expire_at", time.Now()).
			LockUpdate().
			Scan(&apps)
		if err != nil {
			return err
		}

		for _, app := range apps {
			// 支付日志已有成功支付（回调并发窗口）→ 跳过关闭并告警
			paid, err := tx.Model("payment").Ctx(ctx).
				Where("app_id", app.AppId).
				Where("status", consts.PayStatusPaid).
				Count()
			if err != nil {
				return err
			}
			if paid > 0 {
				glog.Errorf(ctx, "[timer] ALARM app=%s expired but paid log exists, skip close", app.AppId)
				continue
			}

			// 关闭订单：status 1 → 9
			_, err = tx.Model("appointment").Ctx(ctx).
				Where("app_id", app.AppId).
				Where("status", consts.StatusPending).
				Data(g.Map{"status": consts.StatusTimeoutClosed, "updated_at": time.Now()}).
				Update()
			if err != nil {
				return err
			}

			// 操作日志（系统动作，staff 为空）
			_, err = tx.Model("oper_log").Ctx(ctx).Data(g.Map{
				"log_id":      "log_" + guid.S(),
				"shop_id":     app.ShopId,
				"staff_id":    "",
				"app_id":      app.AppId,
				"action":      "user_timeout_close",
				"from_status": consts.StatusPending,
				"to_status":   consts.StatusTimeoutClosed,
				"remark":      "待支付超时自动关闭（服务端时间判定）",
				"created_at":  time.Now(),
			}).Insert()
			if err != nil {
				return err
			}

			toRelease = append(toRelease, releaseTask{slotId: app.SlotId, petId: app.PetId})
			closed++
		}
		return nil
	})
	if txErr != nil {
		return 0, txErr
	}

	// 时序红线：仅事务提交成功后释放本单名额
	for _, t := range toRelease {
		if err := quota.Release(ctx, t.slotId, t.petId); err != nil {
			glog.Errorf(ctx, "[timer] ALARM release failed after close, slot=%s pet=%s err=%v", t.slotId, t.petId, err)
		}
	}
	return closed, nil
}
