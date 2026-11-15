// Package shop 通用状态条件更新（CAS）。
//
// 约束口径（agentrules-B端 §13.1 / §16.2）：
// 所有状态变更统一 `UPDATE ... WHERE status = ?` 条件更新，影响行数 0 → 返回对应错误码；
// 禁止直接 UPDATE appointment.status、禁止状态跳级。
package shop

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"
)

// CASUpdateStatus 订单状态条件更新。
// extra 为附加更新字段（如 cancel_apply_at）；返回影响行数。
func CASUpdateStatus(ctx context.Context, appId string, from, to int, extra map[string]interface{}) (int64, error) {
	data := map[string]interface{}{"status": to, "updated_at": time.Now()}
	for k, v := range extra {
		data[k] = v
	}
	r, err := g.DB().Model("appointment").
		Ctx(ctx).
		Where("app_id", appId).
		Where("status", from).
		Data(data).
		Update()
	if err != nil {
		return 0, err
	}
	n, err := r.RowsAffected()
	return n, err
}

// WriteOperLog 写操作日志（只追加）。
// appRef: 订单 app_id 或时段操作时的 slot_id；fromStatus/toStatus 可为 0（无状态变更）。
func WriteOperLog(ctx context.Context, shopId, staffId, appRef, action string, fromStatus, toStatus int, remark string) error {
	_, err := g.DB().Model("oper_log").Ctx(ctx).Data(g.Map{
		"log_id":      "log_" + guid.S(),
		"shop_id":     shopId,
		"staff_id":    staffId,
		"app_id":      appRef,
		"action":      action,
		"from_status": fromStatus,
		"to_status":   toStatus,
		"remark":      remark,
		"created_at":  time.Now(),
	}).Insert()
	return err
}
