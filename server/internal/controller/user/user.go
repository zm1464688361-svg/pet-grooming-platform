// Package user C 端控制器。
package user

import (
	"errors"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	logicUser "pet-grooming/internal/logic/user"
	"pet-grooming/internal/model/response"
)

// Controller C 端接口。
type Controller struct{}

// New 创建 C 端控制器。
func New() *Controller {
	return &Controller{}
}

// SlotList GET /api/user/slot/list 可约时段与服务项目。
func (c *Controller) SlotList(r *ghttp.Request) {
	ctx := r.Context()
	item, err := logicUser.GetItemInfo(ctx)
	if err != nil {
		response.Fail(r, consts.ErrInternal)
		return
	}
	slots, err := logicUser.ListBookableSlots(ctx)
	if err != nil {
		response.Fail(r, consts.ErrInternal)
		return
	}
	response.OK(r, g.Map{"item": item, "slots": slots})
}

// PetList GET /api/user/pet/list 当前用户宠物（user_id 隔离）。
func (c *Controller) PetList(r *ghttp.Request) {
	ctx := r.Context()
	pets, err := logicUser.ListPetsForUser(ctx, auth.UserIdFrom(ctx))
	if err != nil {
		response.Fail(r, consts.ErrInternal)
		return
	}
	response.OK(r, pets)
}

// Cancel POST /api/user/app/cancel 取消预约。
func (c *Controller) Cancel(r *ghttp.Request) {
	var req struct {
		AppId   string `json:"appId"`
		IdemKey string `json:"idemKey"`
		Reason  string `json:"reason"`
	}
	if err := r.Parse(&req); err != nil || req.AppId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicUser.Cancel(r.Context(), auth.UserIdFrom(r.Context()), req.AppId, req.IdemKey, req.Reason)
	if err != nil {
		var biz *logicUser.BizError
		if errors.As(err, &biz) {
			response.Fail(r, biz.Code)
			return
		}
		response.Fail(r, consts.ErrInternal)
		return
	}
	response.OKMsg(r, res.Message, res)
}

// Book POST /api/user/app/book 创建预约（幂等键可选）。
func (c *Controller) Book(r *ghttp.Request) {
	var req struct {
		PetId      string `json:"petId"`
		SlotId     string `json:"slotId"`
		IdemKey    string `json:"idemKey"`
		UserRemark string `json:"userRemark"`
	}
	if err := r.Parse(&req); err != nil {
		response.Fail(r, consts.ErrInternal)
		return
	}
	res, err := logicUser.Book(r.Context(), &logicUser.BookInput{
		UserId:     auth.UserIdFrom(r.Context()), // 登录态注入，不信任前端
		PetId:      req.PetId,
		SlotId:     req.SlotId,
		IdemKey:    req.IdemKey,
		UserRemark: req.UserRemark,
	})
	if err != nil {
		var biz *logicUser.BizError
		if errors.As(err, &biz) {
			response.Fail(r, biz.Code)
			return
		}
		response.Fail(r, consts.ErrInternal)
		return
	}
	response.OKMsg(r, res.Message, res)
}
