// Package cmd 服务启动中间件与健康检查。
package cmd

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/response"
)

// Health 健康检查。
func Health(r *ghttp.Request) {
	r.Response.WriteJson(g.Map{"code": 0, "message": "ok"})
}

// CORS 跨域中间件（本地联调用）。
func CORS(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

// UserIdAuth C 端登录态：解析 X-User-Id 并写入 Context，后续业务强制 user_id 隔离。
func UserIdAuth(r *ghttp.Request) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		response.FailMsg(r, consts.ErrNoPermission, "未登录")
		return
	}
	u, err := auth.ResolveUser(r.Context(), uid)
	if err != nil {
		response.Fail(r, consts.ErrNoPermission)
		return
	}
	if u == nil {
		response.Fail(r, consts.ErrNoPermission)
		return
	}
	r.SetCtx(auth.WithUser(r.Context(), u.UserId))
	r.Middleware.Next()
}

// StaffAuth B 端登录态：解析 X-Staff-Id 得到员工身份（在职校验），写入 Context。
func StaffAuth(r *ghttp.Request) {
	sid := r.Header.Get("X-Staff-Id")
	if sid == "" {
		response.Fail(r, consts.ErrShopForbidden)
		return
	}
	s, err := auth.ResolveStaff(r.Context(), sid)
	if err != nil {
		response.Fail(r, consts.ErrShopForbidden)
		return
	}
	if s == nil {
		// 不存在或已停用员工：视为无权
		response.Fail(r, consts.ErrShopForbidden)
		return
	}
	r.SetCtx(auth.WithStaff(r.Context(), s))
	r.Middleware.Next()
}
