// Package biz 业务错误（C/B 端错误码）。
package biz

import "strconv"

// Error 业务校验错误，Error() 返回错误码字符串便于上层映射固定文案。
type Error struct {
	Code int
}

// Error 实现错误接口。
func (e *Error) Error() string {
	return strconv.Itoa(e.Code)
}

// New 构造业务错误。
func New(code int) *Error {
	return &Error{Code: code}
}
