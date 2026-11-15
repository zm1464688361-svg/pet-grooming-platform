// Package shop B 端退款单列表（客服 / 店长可查，重试仅客服）。
package shop

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/biz"
)

// RefundAdminRow B 端退款单视图。
type RefundAdminRow struct {
	RefundId   string     `json:"refundId"`
	AppId      string     `json:"appId"`
	PetName    string     `json:"petName"`
	RefundType int        `json:"refundType"`
	Amount     int64      `json:"amount"`
	Status     int        `json:"status"`
	StatusName string     `json:"statusName"`
	FailMsg    string     `json:"failMsg"`
	RetryCount int        `json:"retryCount"`
	RefundedAt *time.Time `json:"refundedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// AdminRefundList 本店退款单列表（重试口径见 RefundRetry）。
func AdminRefundList(ctx context.Context, status, page, size int) ([]*RefundAdminRow, int, error) {
	staff := auth.StaffFrom(ctx)
	if staff == nil {
		return nil, 0, biz.New(consts.ErrShopForbidden)
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 20
	}

	m := g.DB().Model("refund r").
		LeftJoin("appointment a", "a.app_id=r.app_id").
		LeftJoin("pet p", "p.pet_id=a.pet_id").
		Where("a.shop_id", staff.ShopId)
	if status > 0 {
		m = m.Where("r.status", status)
	}

	total, err := m.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []*struct {
		RefundId   string     `orm:"refund_id"`
		AppId      string     `orm:"app_id"`
		PetName    string     `orm:"pet_name"`
		RefundType int        `orm:"refund_type"`
		Amount     int64      `orm:"amount"`
		Status     int        `orm:"status"`
		FailMsg    string     `orm:"fail_msg"`
		RetryCount int        `orm:"retry_count"`
		RefundedAt *time.Time `orm:"refunded_at"`
		CreatedAt  time.Time  `orm:"created_at"`
	}
	err = m.Page(page, size).
		Fields("r.refund_id,r.app_id,p.pet_name,r.refund_type,r.amount,r.status,r.fail_msg,r.retry_count,r.refunded_at,r.created_at").
		Order("r.created_at DESC").
		Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*RefundAdminRow, 0, len(rows))
	for _, r := range rows {
		statusName := "退款中"
		if r.Status == consts.RefundStatusSuccess {
			statusName = "退款成功"
		} else if r.Status == consts.RefundStatusFailed {
			statusName = "退款失败"
		}
		list = append(list, &RefundAdminRow{
			RefundId:   r.RefundId,
			AppId:      r.AppId,
			PetName:    r.PetName,
			RefundType: r.RefundType,
			Amount:     r.Amount,
			Status:     r.Status,
			StatusName: statusName,
			FailMsg:    r.FailMsg,
			RetryCount: r.RetryCount,
			RefundedAt: r.RefundedAt,
			CreatedAt:  r.CreatedAt,
		})
	}
	return list, total, nil
}
