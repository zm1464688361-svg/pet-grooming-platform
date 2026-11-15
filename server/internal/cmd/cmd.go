// Package cmd 提供服务启动中间件与健康检查。
package cmd

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
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

// UserIdAuth C 端登录态：从 X-User-Id 请求头解析用户并写入 Context（Task 3 完善）。
func UserIdAuth(r *ghttp.Request) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		r.Response.WriteJson(g.Map{"code": 40001, "message": "未登录"})
		return
	}
	r.SetCtx(r.Context())
	r.Middleware.Next()
}

// StaffAuth B 端登录态：从 X-Staff-Id 请求头解析员工身份与角色（Task 3 完善）。
func StaffAuth(r *ghttp.Request) {
	sid := r.Header.Get("X-Staff-Id")
	if sid == "" {
		r.Response.WriteJson(g.Map{"code": 50001, "message": "未登录"})
		return
	}
	r.Middleware.Next()
}
