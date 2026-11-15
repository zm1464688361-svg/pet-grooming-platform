// Package user C 端控制器。
package user

import (
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
