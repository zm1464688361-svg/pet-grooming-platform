// Package shop B 端未到店标记（店长）。
//
// 约束口径（agentrules-B端 §7）：
//   - 前置：status=2 + 服务端时间晚于 slot.end_time + 未核销 + 该时段无 status=3；
//   - 时段未结束 → 50005；noshow_reason 必填（前后端双重非空，无默认值）→ 50006；
//   - 标记后 → 8 终态：名额不释放、不自动退款、不可重复标记、不可提交取消；
//   - 重复标记 → 幂等提示"已标记未到店"；
//   - 资金口径：计入已收款，不计入已完成服务收入；
//   - oper_log 写 NOSHOW。
package shop

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/biz"
)

// 未到店固定文案（agentrules-B端 §17）
const (
	MsgNoShowMarked   = "已标记未到店，本次不自动退款"
	MsgNoShowRepeated = "已标记未到店"
	MsgSlotNotEnded   = "时段尚未结束，暂不可标记未到店"
)

// NoShowResult 未到店标记结果。
type NoShowResult struct {
	AppId   string `json:"appId"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// MarkNoShow 标记未到店（店长）。
func MarkNoShow(ctx context.Context, appId, reason string) (*NoShowResult, error) {
	staff := auth.StaffFrom(ctx)
	// 角色校验：仅店长(1)（前台/服务/客服越权 → 40001）
	if staff == nil || staff.Role != consts.RoleManager {
		return nil, biz.New(consts.ErrNoPermission)
	}

	app, err := loadOwnedApp(ctx, staff, appId)
	if err != nil {
		return nil, err
	}

	switch app.Status {
	case consts.StatusConfirmed:
		// 正常前置
	case consts.StatusNoShow:
		// 重复标记：幂等提示
		return &NoShowResult{AppId: appId, Status: consts.StatusNoShow, Message: MsgNoShowRepeated}, nil
	default:
		return nil, biz.New(consts.ErrStatusConflict) // 已核销/取消/审核中(走 50004 分支)等
	}

	// noshow_reason 必填：前后端双重非空，禁止默认值
	if strings.TrimSpace(reason) == "" {
		return nil, biz.New(consts.ErrNoShowReasonNeed)
	}

	// 时段未结束 → 50005（服务端时间与 slot.end_time 比较）
	vEnd, err := g.DB().Model("time_slot").Where("slot_id", app.SlotId).Value("end_time")
	if err != nil {
		return nil, err
	}
	if vEnd == nil || vEnd.IsNil() {
		return nil, biz.New(consts.ErrShopForbidden)
	}
	if !time.Now().After(vEnd.Time()) {
		return nil, biz.New(consts.ErrSlotNotEnded)
	}

	// 同时段存在 status=3 → 50004（状态 3 期间禁止标记未到店）
	if err := blockIfSlotAudit(ctx, staff.ShopId, app.SlotId); err != nil {
		return nil, err
	}

	// 条件更新 2→8
	n, err := CASUpdateStatus(ctx, appId, consts.StatusConfirmed, consts.StatusNoShow, map[string]interface{}{
		"noshow_at":     time.Now(),
		"noshow_reason": reason,
	})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		cur, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status")
		if cur != nil && cur.Int() == consts.StatusNoShow {
			return &NoShowResult{AppId: appId, Status: consts.StatusNoShow, Message: MsgNoShowRepeated}, nil
		}
		return nil, biz.New(consts.ErrStatusConflict)
	}

	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, appId, "NOSHOW", consts.StatusConfirmed, consts.StatusNoShow, "标记未到店："+reason); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[noshow] app=%s by=%s reason_len=%d", appId, staff.StaffId, len(reason))
	return &NoShowResult{AppId: appId, Status: consts.StatusNoShow, Message: MsgNoShowMarked}, nil
}
