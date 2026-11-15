// Package shop B 端服务进度登记（服务人员 / 店长）。
//
// 约束口径（agentrules-B端 §6）：
//   - 只能 5→6（写 service_start_at）、6→7（写 service_finish_at），禁止跳级 → 50009；
//   - 未经核销的订单不得登记开始；
//   - 重复点击开始/完成 → 幂等固定文案（"已登记开始" / "已完成登记"），收入只计一次；
//   - 仅状态 7 计入已完成服务收入（台账口径，聚合在台账模块）；
//   - 该时段存在 status=3 → 50004（状态 3 期间禁止服务登记）；
//   - oper_log 写 START / FINISH。
package shop

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/biz"
)

// 服务进度固定文案（agentrules-B端 §13/§17）
const (
	MsgStartRegistered = "已登记开始"
	MsgFinishDone      = "已完成登记"
)

// ServiceResult 服务进度结果。
type ServiceResult struct {
	AppId   string `json:"appId"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// requireServiceRole 服务登记角色：仅服务(3)/店长(1)。
func requireServiceRole(staff *auth.StaffIdentity) error {
	if staff == nil || (staff.Role != consts.RoleService && staff.Role != consts.RoleManager) {
		return biz.New(consts.ErrNoPermission)
	}
	return nil
}

// loadOwnedApp 校验订单归属本店（50001）。
func loadOwnedApp(ctx context.Context, staff *auth.StaffIdentity, appId string) (*struct {
	AppId  string `orm:"app_id"`
	ShopId string `orm:"shop_id"`
	Status int    `orm:"status"`
	SlotId string `orm:"slot_id"`
}, error) {
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
	return &app, nil
}

// StartService 登记服务开始（5→6）。
func StartService(ctx context.Context, appId string) (*ServiceResult, error) {
	staff := auth.StaffFrom(ctx)
	if err := requireServiceRole(staff); err != nil {
		return nil, err
	}
	app, err := loadOwnedApp(ctx, staff, appId)
	if err != nil {
		return nil, err
	}

	switch app.Status {
	case consts.StatusVerified:
		// 正常路径：5→6
	case consts.StatusInService:
		return &ServiceResult{AppId: appId, Status: consts.StatusInService, Message: MsgStartRegistered}, nil // 幂等
	case consts.StatusPending, consts.StatusConfirmed:
		return nil, biz.New(consts.ErrSkipProgress) // 未经核销
	default:
		return nil, biz.New(consts.ErrStatusConflict)
	}

	if err := blockIfSlotAudit(ctx, staff.ShopId, app.SlotId); err != nil {
		return nil, err
	}

	n, err := CASUpdateStatus(ctx, appId, consts.StatusVerified, consts.StatusInService,
		map[string]interface{}{"service_start_at": time.Now()})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		cur, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status")
		if cur != nil && cur.Int() == consts.StatusInService {
			return &ServiceResult{AppId: appId, Status: consts.StatusInService, Message: MsgStartRegistered}, nil
		}
		return nil, biz.New(consts.ErrStatusConflict)
	}

	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, appId, "START", consts.StatusVerified, consts.StatusInService, "登记服务开始"); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[service] start app=%s by=%s", appId, staff.StaffId)
	return &ServiceResult{AppId: appId, Status: consts.StatusInService, Message: MsgStartRegistered}, nil
}

// FinishService 登记服务完成（6→7）。
func FinishService(ctx context.Context, appId string) (*ServiceResult, error) {
	staff := auth.StaffFrom(ctx)
	if err := requireServiceRole(staff); err != nil {
		return nil, err
	}
	app, err := loadOwnedApp(ctx, staff, appId)
	if err != nil {
		return nil, err
	}

	switch app.Status {
	case consts.StatusInService:
		// 正常路径：6→7
	case consts.StatusFinished:
		return &ServiceResult{AppId: appId, Status: consts.StatusFinished, Message: MsgFinishDone}, nil // 幂等：收入只计一次
	case consts.StatusVerified:
		return nil, biz.New(consts.ErrSkipProgress) // 跳级
	default:
		return nil, biz.New(consts.ErrStatusConflict)
	}

	if err := blockIfSlotAudit(ctx, staff.ShopId, app.SlotId); err != nil {
		return nil, err
	}

	n, err := CASUpdateStatus(ctx, appId, consts.StatusInService, consts.StatusFinished,
		map[string]interface{}{"service_finish_at": time.Now()})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		cur, _ := g.DB().Model("appointment").Where("app_id", appId).Value("status")
		if cur != nil && cur.Int() == consts.StatusFinished {
			return &ServiceResult{AppId: appId, Status: consts.StatusFinished, Message: MsgFinishDone}, nil
		}
		return nil, biz.New(consts.ErrStatusConflict)
	}

	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, appId, "FINISH", consts.StatusInService, consts.StatusFinished, "登记服务完成"); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[service] finish app=%s by=%s", appId, staff.StaffId)
	return &ServiceResult{AppId: appId, Status: consts.StatusFinished, Message: MsgFinishDone}, nil
}

// blockIfSlotAudit 该时段存在 status=3 → 50004。
func blockIfSlotAudit(ctx context.Context, shopId, slotId string) error {
	n, err := g.DB().Model("appointment").
		Where("shop_id", shopId).
		Where("slot_id", slotId).
		Where("status", consts.StatusCancelAudit).
		Count()
	if err != nil {
		return err
	}
	if n > 0 {
		return biz.New(consts.ErrAuditBlocking)
	}
	return nil
}
