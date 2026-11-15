// Package user C 端退款单查询（只读，user_id 隔离）。
//
// 退款状态独立于预约状态展示（agentrules §15/16）：退款失败不恢复预约、不重占名额。
package user

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
)

// RefundView C 端退款单视图。
type RefundView struct {
	RefundId      string     `json:"refundId"`
	AppId         string     `json:"appId"`
	RefundType    int        `json:"refundType"`
	RefundName    string     `json:"refundName"`
	Amount        int64      `json:"amount"`
	Status        int        `json:"status"`
	StatusName    string     `json:"statusName"`
	FailMsg       string     `json:"failMsg"`
	RetryCount    int        `json:"retryCount"`
	RefundedAt    *time.Time `json:"refundedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	PetName       string     `json:"petName"`
	AppStatus     int        `json:"appStatus"`
	AppStatusName string     `json:"appStatusName"`
}

// ListMyRefunds 查询当前用户全部退款单（强制 user_id 隔离）。
func ListMyRefunds(ctx context.Context, userId string) ([]*RefundView, error) {
	var rows []*struct {
		RefundId   string     `orm:"refund_id"`
		AppId      string     `orm:"app_id"`
		RefundType int        `orm:"refund_type"`
		Amount     int64      `orm:"amount"`
		Status     int        `orm:"status"`
		FailMsg    string     `orm:"fail_msg"`
		RetryCount int        `orm:"retry_count"`
		RefundedAt *time.Time `orm:"refunded_at"`
		CreatedAt  time.Time  `orm:"created_at"`
		PetName    string     `orm:"pet_name"`
		AppStatus  int        `orm:"app_status"`
	}
	err := g.DB().Model("refund r").
		LeftJoin("appointment a", "a.app_id=r.app_id").
		LeftJoin("pet p", "p.pet_id=a.pet_id").
		Fields("r.refund_id,r.app_id,r.refund_type,r.amount,r.status,r.fail_msg,r.retry_count,r.refunded_at,r.created_at,p.pet_name,a.status AS app_status").
		Where("a.user_id", userId). // 数据隔离红线
		Order("r.created_at DESC").
		Scan(&rows)
	if err != nil {
		return nil, err
	}

	views := make([]*RefundView, 0, len(rows))
	for _, r := range rows {
		typeName := "取消退款"
		if r.RefundType == consts.RefundTypeDuplicate {
			typeName = "重复支付退款"
		} else if r.RefundType == consts.RefundTypeException {
			typeName = "支付异常退款"
		}
		statusName := "退款中"
		if r.Status == consts.RefundStatusSuccess {
			statusName = "退款成功"
		} else if r.Status == consts.RefundStatusFailed {
			statusName = "退款失败"
		}
		views = append(views, &RefundView{
			RefundId:      r.RefundId,
			AppId:         r.AppId,
			RefundType:    r.RefundType,
			RefundName:    typeName,
			Amount:        r.Amount,
			Status:        r.Status,
			StatusName:    statusName,
			FailMsg:       r.FailMsg,
			RetryCount:    r.RetryCount,
			RefundedAt:    r.RefundedAt,
			CreatedAt:     r.CreatedAt,
			PetName:       r.PetName,
			AppStatus:     r.AppStatus,
			AppStatusName: consts.AppointmentStatusNames[r.AppStatus],
		})
	}
	return views, nil
}
