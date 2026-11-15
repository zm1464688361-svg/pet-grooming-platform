// Package response 统一响应结构。
package response

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"pet-grooming/internal/consts"
)

// OK 成功响应：{"code":0,"message":"ok","data":...}
func OK(r *ghttp.Request, data interface{}) {
	r.Response.WriteJson(gMap(0, "ok", data))
}

// OKMsg 成功响应（自定义文案，如固定业务文案）。
func OKMsg(r *ghttp.Request, msg string, data interface{}) {
	r.Response.WriteJson(gMap(0, msg, data))
}

// Fail 失败响应（错误码 → 固定文案，禁止自创文案）。
func Fail(r *ghttp.Request, code int) {
	msg, ok := consts.ErrorMessages[code]
	if !ok {
		msg = "系统繁忙，请稍后重试"
	}
	r.Response.WriteJson(gMap(code, msg, nil))
}

// FailMsg 失败响应（自定义文案，用于业务固定文案）。
func FailMsg(r *ghttp.Request, code int, msg string) {
	r.Response.WriteJson(gMap(code, msg, nil))
}

func gMap(code int, msg string, data interface{}) map[string]interface{} {
	return map[string]interface{}{"code": code, "message": msg, "data": data}
}
