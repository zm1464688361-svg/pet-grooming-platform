// Package shop B 端到店核销（前台 / 店长）。
//
// 约束口径（agentrules-B端 §5）：
//   - 三重校验：本店员工（shop_id 一致且角色为前台或店长）、status=2、该时段不存在 status=3；
//   - 状态 3 一律拒绝 → 50004，禁止先核销后补审；
//   - 核销成功 → 5，名额永久占用，永久退出取消退款流程；
//   - 重复核销：条件更新影响 0 行 → 50003（收入不重复）；
//   - 核销与取消并发：仅先落库者成功，另一方按最新状态失败。
package shop

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/biz"
)

// 核销固定文案（agentrules-B端 §17）
const (
	MsgVerifySuccess = "核销成功，该预约已进入服务流程"
	MsgAuditBlocking = "该预约存在待处理取消申请，暂不可核销"
)

// VerifyResult 核销结果。
type VerifyResult struct {
	AppId   string `json:"appId"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// VerifyAppointment 核销预约。
func VerifyAppointment(ctx context.Context, appId string) (*VerifyResult, error) {
	staff := auth.StaffFrom(ctx)
	if staff == nil {
		return nil, biz.New(consts.ErrShopForbidden)
	}
	// 角色校验：仅前台(2)/店长(1)；服务(3)/客服(4)越权 → 40001
	if staff.Role != consts.RoleFront && staff.Role != consts.RoleManager {
		return nil, biz.New(consts.ErrNoPermission)
	}

	// 订单归属校验（本店）→ 50001
	var app struct {
		AppId  string `orm:"app_id"`
		ShopId string `orm:"shop_id"`
		Status int    `orm:"status"`
		SlotId string `orm:"slot_id"`
	}
	err := g.DB().Model("appointment").
		Fields("app_id,shop_id,status,slot_id").
		Where("app_id", appId).
		Scan(&app)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if app.AppId == "" || app.ShopId != staff.ShopId {
		return nil, biz.New(consts.ErrShopForbidden)
	}

	// 该时段存在 status=3 待审核取消申请 → 50004（审核期间该时段暂无法核销）
	auditCnt, err := g.DB().Model("appointment").
		Where("shop_id", staff.ShopId).
		Where("slot_id", app.SlotId).
		Where("status", consts.StatusCancelAudit).
		Count()
	if err != nil {
		return nil, err
	}
	if auditCnt > 0 {
		return nil, biz.New(consts.ErrAuditBlocking)
	}

	// 条件更新 2→5
	n, err := CASUpdateStatus(ctx, appId, consts.StatusConfirmed, consts.StatusVerified, nil)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		// 按最新状态返回：3 → 50004，其余 → 50003
		cur, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status")
		if cur != nil && cur.Int() == consts.StatusCancelAudit {
			return nil, biz.New(consts.ErrAuditBlocking)
		}
		return nil, biz.New(consts.ErrStatusConflict)
	}

	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, appId, "VERIFY", consts.StatusConfirmed, consts.StatusVerified, "到店核销"); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[verify] app=%s by=%s(%s)", appId, staff.StaffId, staff.StaffName)
	return &VerifyResult{AppId: appId, Status: consts.StatusVerified, Message: MsgVerifySuccess}, nil
}
