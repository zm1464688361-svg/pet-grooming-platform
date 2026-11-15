// Package shop B 端取消审批（客服 / 店长兼管）与退款重试（客服）。
//
// 约束口径（agentrules-B端 §8-9）：
//   - 审批列表仅 status=3，按申请时间正序，DB 分页；≥2h 自动取消不进列表；
//   - 同意：3→4，事务提交后释放名额，生成全额退款单（refund_type=1，模拟渠道立即打款成功）；
//   - 驳回：3→2，名额继续占用，恢复可核销；
//   - 一条申请只处理一次，重复处理 → 50007；
//   - 退款重试：仅客服；店长/其他角色 → 50008；只操作退款单，不改预约状态、不动名额；
//     Redis SETNX 60s 防并发重复提交；记录 retry_count、fail_msg、refunded_at；
//   - oper_log 写 AUDIT / RETRY。
package shop

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/logic/quota"
	"pet-grooming/internal/model/biz"
)

// 审批与退款固定文案（agentrules-B端 §17）
const (
	MsgAuditApproved    = "已同意取消，退款已发起"
	MsgAuditRejected    = "已驳回，预约恢复有效，可正常到店"
	MsgRefundRetried    = "已重新发起退款，请稍后查看结果"
	MsgRefundProcessing = "退款处理中，请稍后查看结果"
)

// AuditRow 审批列表项。
type AuditRow struct {
	AppId         string    `json:"appId"`
	UserPhone     string    `json:"userPhone"`
	PetName       string    `json:"petName"`
	StartTime     time.Time `json:"startTime"`
	Price         int64     `json:"price"`
	Status        int       `json:"status"`
	StatusName    string    `json:"statusName"`
	CancelApplyAt time.Time `json:"cancelApplyAt"`
	CancelReason  string    `json:"cancelReason"`
}

type entityAuditApp struct {
	AppId         string    `orm:"app_id"`
	Phone         string    `orm:"phone"`
	PetName       string    `orm:"pet_name"`
	StartTime     time.Time `orm:"start_time"`
	Price         int64     `orm:"price"`
	Status        int       `orm:"status"`
	CancelApplyAt time.Time `orm:"cancel_apply_at"`
	CancelReason  string    `orm:"cancel_reason"`
}

// requireAuditRole 审批角色：客服(4)或店长(1，兼管留痕)。
func requireAuditRole(staff *auth.StaffIdentity) error {
	if staff == nil || (staff.Role != consts.RoleSupport && staff.Role != consts.RoleManager) {
		return biz.New(consts.ErrNoPermission)
	}
	return nil
}

// AuditList 审批列表（status=3，申请时间正序）。
func AuditList(ctx context.Context, page, size int) ([]*AuditRow, int, error) {
	staff := auth.StaffFrom(ctx)
	if staff == nil {
		return nil, 0, biz.New(consts.ErrShopForbidden)
	}
	if err := requireAuditRole(staff); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 20
	}

	m := g.DB().Model("appointment a").
		LeftJoin("user u", "u.user_id=a.user_id").
		LeftJoin("pet p", "p.pet_id=a.pet_id").
		LeftJoin("time_slot s", "s.slot_id=a.slot_id").
		Where("a.shop_id", staff.ShopId).
		Where("a.status", consts.StatusCancelAudit)

	total, err := m.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []*entityAuditApp
	err = m.Page(page, size).
		Fields("a.app_id,a.price,a.status,a.cancel_apply_at,a.cancel_reason,u.phone,p.pet_name,s.start_time").
		Order("a.cancel_apply_at ASC"). // 申请时间正序
		Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*AuditRow, 0, len(rows))
	for _, r := range rows {
		list = append(list, &AuditRow{
			AppId:         r.AppId,
			UserPhone:     r.Phone,
			PetName:       r.PetName,
			StartTime:     r.StartTime,
			Price:         r.Price,
			Status:        r.Status,
			StatusName:    consts.AppointmentStatusNames[r.Status],
			CancelApplyAt: r.CancelApplyAt,
			CancelReason:  r.CancelReason,
		})
	}
	return list, total, nil
}

// AuditApprove 审批同意：3→4，释放名额，生成全额退款单（模拟渠道立即打款成功）。
func AuditApprove(ctx context.Context, appId string) (*ServiceResult, error) {
	staff := auth.StaffFrom(ctx)
	if err := requireAuditRole(staff); err != nil {
		return nil, err
	}
	app, err := loadOwnedApp(ctx, staff, appId)
	if err != nil {
		return nil, err
	}

	// 条件更新 3→4（重复处理 → 50007）
	n, err := CASUpdateStatus(ctx, appId, consts.StatusCancelAudit, consts.StatusCancelled, map[string]interface{}{
		"audit_result":   consts.AuditApprove,
		"audit_at":       time.Now(),
		"audit_staff_id": staff.StaffId,
	})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, biz.New(consts.ErrAuditDone)
	}

	// 全额退款单：模拟渠道立即打款成功
	refundId := "ref_" + guid.S()
	_, err = g.DB().Model("refund").Data(g.Map{
		"refund_id":   refundId,
		"app_id":      appId,
		"refund_type": consts.RefundTypeCancel,
		"amount":      appPrice(ctx, appId),
		"status":      consts.RefundStatusSuccess,
		"refunded_at": time.Now(),
		"created_at":  time.Now(),
	}).Insert()
	if err != nil {
		return nil, err
	}

	// 事务已提交：释放名额（4 个释放场景之一：客服审批同意）
	if err := quota.Release(ctx, app.SlotId, appPetId(ctx, appId)); err != nil {
		glog.Errorf(ctx, "[audit] ALARM release failed slot=%s err=%v", app.SlotId, err)
		_ = quota.RebuildQuota(ctx, app.SlotId)
	}

	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, appId, "AUDIT", consts.StatusCancelAudit, consts.StatusCancelled, "客服审批同意，全额退款已发起"); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[audit] approved app=%s by=%s refund=%s", appId, staff.StaffId, refundId)
	return &ServiceResult{AppId: appId, Status: consts.StatusCancelled, Message: MsgAuditApproved}, nil
}

// AuditReject 审批驳回：3→2，名额继续占用，恢复可核销。
func AuditReject(ctx context.Context, appId, reason string) (*ServiceResult, error) {
	staff := auth.StaffFrom(ctx)
	if err := requireAuditRole(staff); err != nil {
		return nil, err
	}
	if _, err := loadOwnedApp(ctx, staff, appId); err != nil {
		return nil, err
	}

	n, err := CASUpdateStatus(ctx, appId, consts.StatusCancelAudit, consts.StatusConfirmed, map[string]interface{}{
		"audit_result":   consts.AuditReject,
		"audit_at":       time.Now(),
		"audit_staff_id": staff.StaffId,
	})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, biz.New(consts.ErrAuditDone)
	}

	remark := "客服审批驳回"
	if reason != "" {
		remark += "：" + reason
	}
	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, appId, "AUDIT", consts.StatusCancelAudit, consts.StatusConfirmed, remark); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[audit] rejected app=%s by=%s reason=%s", appId, staff.StaffId, reason)
	return &ServiceResult{AppId: appId, Status: consts.StatusConfirmed, Message: MsgAuditRejected}, nil
}

// appPrice 查询订单金额。
func appPrice(ctx context.Context, appId string) int64 {
	v, _ := g.DB().Model("appointment").Where("app_id", appId).Value("price")
	if v == nil {
		return 0
	}
	return v.Int64()
}

// appPetId 查询订单宠物。
func appPetId(ctx context.Context, appId string) string {
	v, _ := g.DB().Model("appointment").Where("app_id", appId).Value("pet_id")
	if v == nil {
		return ""
	}
	return v.String()
}

// RefundRetryResult 退款重试结果。
type RefundRetryResult struct {
	RefundId string `json:"refundId"`
	Status   int    `json:"status"`
	Message  string `json:"message"`
}

// RefundRetry 退款重试（仅客服）。
func RefundRetry(ctx context.Context, refundId string) (*RefundRetryResult, error) {
	staff := auth.StaffFrom(ctx)
	// 角色校验：仅客服(4)；店长只读 → 50008
	if staff == nil || staff.Role != consts.RoleSupport {
		return nil, biz.New(consts.ErrRefundForbidden)
	}

	// 退款单归属本店订单
	var refund struct {
		RefundId string `orm:"refund_id"`
		AppId    string `orm:"app_id"`
		Status   int    `orm:"status"`
	}
	err := g.DB().Model("refund r").
		Fields("r.refund_id,r.app_id,r.status").
		Where("r.refund_id", refundId).
		Scan(&refund)
	if err != nil {
		return nil, err
	}
	if refund.RefundId == "" {
		return nil, biz.New(consts.ErrStatusConflict)
	}
	appShop, _ := g.DB().Model("appointment").Where("app_id", refund.AppId).Value("shop_id")
	if appShop == nil || appShop.String() != staff.ShopId {
		return nil, biz.New(consts.ErrShopForbidden)
	}

	// 已成功 → 50003；待退款/失败才可重试
	if refund.Status == consts.RefundStatusSuccess {
		return nil, biz.New(consts.ErrStatusConflict)
	}

	// Redis SETNX 60s：并发重复提交 → 提示处理中
	res, err := g.Redis().Do(ctx, "SET", "refund:retry:"+refundId, "1", "NX", "EX", 60)
	if err != nil {
		return nil, err
	}
	if res.String() != "OK" {
		return &RefundRetryResult{RefundId: refundId, Status: refund.Status, Message: MsgRefundProcessing}, nil
	}

	// 模拟渠道打款：重试成功（重试不改预约状态、不动名额）
	_, err = g.DB().Model("refund").
		Where("refund_id", refundId).
		WhereIn("status", g.Slice{consts.RefundStatusPending, consts.RefundStatusFailed}).
		Data(g.Map{
			"status":      consts.RefundStatusSuccess,
			"retry_count": gdb.Raw("retry_count + 1"),
			"fail_msg":    "",
			"refunded_at": time.Now(),
		}).
		Update()
	if err != nil {
		_, _ = g.Redis().Do(ctx, "DEL", "refund:retry:"+refundId)
		return nil, err
	}

	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, refund.AppId, "RETRY", refund.Status, consts.RefundStatusSuccess, "退款重试成功（模拟渠道）"); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[refund] retried refund=%s by=%s", refundId, staff.StaffId)
	return &RefundRetryResult{RefundId: refundId, Status: consts.RefundStatusSuccess, Message: MsgRefundRetried}, nil
}

// gdbInc 已由 gdb.Raw 替代（retry_count 原子自增）。
