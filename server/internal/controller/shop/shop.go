// Package shop B 端控制器（时段管理等）。
package shop

import (
	"errors"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"pet-grooming/internal/consts"
	logicShop "pet-grooming/internal/logic/shop"
	"pet-grooming/internal/model/biz"
	"pet-grooming/internal/model/response"
)

// Controller B 端接口。
type Controller struct{}

// New 创建 B 端控制器。
func New() *Controller {
	return &Controller{}
}

// replyFail 统一错误应答：业务错误映射固定文案，其余 500。
func replyFail(r *ghttp.Request, err error) {
	var bizErr *biz.Error
	if errors.As(err, &bizErr) {
		response.Fail(r, bizErr.Code)
		return
	}
	response.Fail(r, consts.ErrInternal)
}

// SlotCreate POST /api/shop/slot/create 店长发布时段。
func (c *Controller) SlotCreate(r *ghttp.Request) {
	var req struct {
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	}
	if err := r.Parse(&req); err != nil {
		response.Fail(r, consts.ErrInternal)
		return
	}
	start, err1 := parseTime(req.StartTime)
	end, err2 := parseTime(req.EndTime)
	if err1 != nil || err2 != nil {
		response.Fail(r, consts.ErrSlotUnavailable)
		return
	}
	slotId, err := logicShop.CreateSlot(r.Context(), start, end)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, "时段发布成功", g.Map{"slotId": slotId})
}

// SlotList GET /api/shop/slot/list 本店时段列表（分页，date=YYYY-MM-DD 可选）。
func (c *Controller) SlotList(r *ghttp.Request) {
	page := r.Get("page").Int()
	size := r.Get("size").Int()
	date := r.Get("date").String()
	list, err := logicShop.ListShopSlots(r.Context(), page, size, date)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OK(r, g.Map{"list": list})
}

// SlotPause POST /api/shop/slot/pause 暂停时段（店长）。
func (c *Controller) SlotPause(r *ghttp.Request) {
	slotId := r.Get("slotId").String()
	if err := logicShop.PauseSlot(r.Context(), slotId); err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, "时段已暂停", nil)
}

// SlotResume POST /api/shop/slot/resume 恢复时段（店长）。
func (c *Controller) SlotResume(r *ghttp.Request) {
	slotId := r.Get("slotId").String()
	if err := logicShop.ResumeSlot(r.Context(), slotId); err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, "时段已恢复", nil)
}

// SlotUpdate POST /api/shop/slot/update 修改时段时间（店长）。
func (c *Controller) SlotUpdate(r *ghttp.Request) {
	var req struct {
		SlotId    string `json:"slotId"`
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	}
	if err := r.Parse(&req); err != nil || req.SlotId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	start, err1 := parseTime(req.StartTime)
	end, err2 := parseTime(req.EndTime)
	if err1 != nil || err2 != nil {
		response.Fail(r, consts.ErrSlotUnavailable)
		return
	}
	if err := logicShop.UpdateSlotTime(r.Context(), req.SlotId, start, end); err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, "时段已更新", nil)
}

// SlotDelete POST /api/shop/slot/delete 删除时段（店长）。
func (c *Controller) SlotDelete(r *ghttp.Request) {
	slotId := r.Get("slotId").String()
	if err := logicShop.DeleteSlot(r.Context(), slotId); err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, "时段已删除", nil)
}

// AppointmentList GET /api/shop/app/list 预约查询（全角色，分页默认 20）。
func (c *Controller) AppointmentList(r *ghttp.Request) {
	var req struct {
		Date    string `json:"date"`
		Status  int    `json:"status"`
		PetName string `json:"petName"`
		Keyword string `json:"keyword"`
		Page    int    `json:"page"`
		Size    int    `json:"size"`
	}
	_ = r.Parse(&req)
	list, total, err := logicShop.QueryAppointments(
		r.Context(), req.Date, req.Status, req.PetName, req.Keyword, req.Page, req.Size,
	)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OK(r, g.Map{"list": list, "total": total})
}

// PetHistory GET /api/shop/pet/history 宠物在本店历史预约（只读）。
func (c *Controller) PetHistory(r *ghttp.Request) {
	petId := r.Get("petId").String()
	if petId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	page := r.Get("page").Int()
	size := r.Get("size").Int()
	list, total, err := logicShop.PetHistory(r.Context(), petId, page, size)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OK(r, g.Map{"list": list, "total": total})
}

// AppVerify POST /api/shop/app/verify 到店核销（前台/店长）。
func (c *Controller) AppVerify(r *ghttp.Request) {
	var req struct {
		AppId string `json:"appId"`
	}
	if err := r.Parse(&req); err != nil || req.AppId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicShop.VerifyAppointment(r.Context(), req.AppId)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, res.Message, res)
}

// ServiceStart POST /api/shop/app/service/start 登记服务开始（服务人员/店长）。
func (c *Controller) ServiceStart(r *ghttp.Request) {
	var req struct {
		AppId string `json:"appId"`
	}
	if err := r.Parse(&req); err != nil || req.AppId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicShop.StartService(r.Context(), req.AppId)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, res.Message, res)
}

// ServiceFinish POST /api/shop/app/service/finish 登记服务完成（服务人员/店长）。
func (c *Controller) ServiceFinish(r *ghttp.Request) {
	var req struct {
		AppId string `json:"appId"`
	}
	if err := r.Parse(&req); err != nil || req.AppId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicShop.FinishService(r.Context(), req.AppId)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, res.Message, res)
}

// AuditList GET /api/shop/audit/list 取消审批列表（客服/店长，申请时间正序）。
func (c *Controller) AuditList(r *ghttp.Request) {
	list, total, err := logicShop.AuditList(r.Context(), r.Get("page").Int(), r.Get("size").Int())
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OK(r, g.Map{"list": list, "total": total})
}

// AuditApprove POST /api/shop/audit/approve 审批同意（客服/店长）。
func (c *Controller) AuditApprove(r *ghttp.Request) {
	var req struct {
		AppId string `json:"appId"`
	}
	if err := r.Parse(&req); err != nil || req.AppId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicShop.AuditApprove(r.Context(), req.AppId)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, res.Message, res)
}

// AuditReject POST /api/shop/audit/reject 审批驳回（客服/店长）。
func (c *Controller) AuditReject(r *ghttp.Request) {
	var req struct {
		AppId  string `json:"appId"`
		Reason string `json:"reason"`
	}
	if err := r.Parse(&req); err != nil || req.AppId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicShop.AuditReject(r.Context(), req.AppId, req.Reason)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, res.Message, res)
}

// RefundRetry POST /api/shop/refund/retry 退款重试（仅客服）。
func (c *Controller) RefundRetry(r *ghttp.Request) {
	var req struct {
		RefundId string `json:"refundId"`
	}
	if err := r.Parse(&req); err != nil || req.RefundId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicShop.RefundRetry(r.Context(), req.RefundId)
	if err != nil {
		replyFail(r, err)
		return
	}
	response.OKMsg(r, res.Message, res)
}

// parseTime 解析前端时间参数（仅解析展示层输入，业务判定一律取服务端时间）。
func parseTime(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
}
