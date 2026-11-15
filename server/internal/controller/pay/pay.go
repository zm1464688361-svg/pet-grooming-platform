// Package pay 支付控制器（发起支付 / 模拟渠道回调）。
package pay

import (
	"errors"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	logicPay "pet-grooming/internal/logic/pay"
	"pet-grooming/internal/model/biz"
	"pet-grooming/internal/model/response"
)

// Controller 支付接口。
type Controller struct{}

// New 创建支付控制器。
func New() *Controller {
	return &Controller{}
}

// Create POST /api/pay/create 发起支付（C 端登录态）。
func (c *Controller) Create(r *ghttp.Request) {
	var req struct {
		AppId string `json:"appId"`
	}
	if err := r.Parse(&req); err != nil || req.AppId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	paymentId, err := logicPay.InitiatePay(r.Context(), auth.UserIdFrom(r.Context()), req.AppId)
	if err != nil {
		replyBiz(r, err)
		return
	}
	response.OKMsg(r, consts.MsgPayProcessing, g.Map{"paymentId": paymentId})
}

// MockNotify POST /api/pay/mock/notify 模拟支付渠道回调（演示用，无鉴权）。
func (c *Controller) MockNotify(r *ghttp.Request) {
	var req struct {
		PaymentId     string `json:"paymentId"`
		TransactionId string `json:"transactionId"`
		Success       bool   `json:"success"`
		Amount        int64  `json:"amount"`
	}
	if err := r.Parse(&req); err != nil || req.PaymentId == "" || req.TransactionId == "" {
		response.Fail(r, consts.ErrInternal)
		return
	}
	if err := logicPay.HandleNotify(r.Context(), logicPay.NotifyInput{
		PaymentId:     req.PaymentId,
		TransactionId: req.TransactionId,
		Success:       req.Success,
		Amount:        req.Amount,
	}); err != nil {
		replyBiz(r, err)
		return
	}
	response.OKMsg(r, consts.MsgPaySuccess, nil)
}

func replyBiz(r *ghttp.Request, err error) {
	var bizErr *biz.Error
	if errors.As(err, &bizErr) {
		response.Fail(r, bizErr.Code)
		return
	}
	response.Fail(r, consts.ErrInternal)
}
