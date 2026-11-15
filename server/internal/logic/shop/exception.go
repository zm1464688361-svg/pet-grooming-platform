// Package shop B 端支付异常单（店长 / 客服）。
//
// 约束口径（agentrules-B端 §10）：
//   - 异常单由支付回调自动登记（关闭后支付 type=1 / 重复扣款 type=2），进入待处理清单；
//   - 处置三步：登记 pay_exception → 发起全额退款（refund_type=3）→ 保持预约关闭、名额不恢复；
//   - 全程不触碰 appointment.status；
//   - 已处理不可重复处理 → 50010；
//   - 列表强制 shop_id 过滤 + DB 分页。
package shop

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/biz"
)

// 支付异常固定文案（agentrules-B端 §17）
const MsgExceptionProcessed = "该笔支付发生在订单关闭之后，已登记异常并发起退款"

// ExceptionRow 异常单列表项。
type ExceptionRow struct {
	ExcId         string    `json:"excId"`
	AppId         string    `json:"appId"`
	TransactionId string    `json:"transactionId"`
	Amount        int64     `json:"amount"`
	ExcType       int       `json:"excType"`
	Status        int       `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	AppStatusName string    `json:"appStatusName"` // 关联预约的当前状态文案
}

type entityPayException struct {
	ExcId         string    `orm:"exc_id"`
	ShopId        string    `orm:"shop_id"`
	AppId         string    `orm:"app_id"`
	TransactionId string    `orm:"transaction_id"`
	Amount        int64     `orm:"amount"`
	ExcType       int       `orm:"exc_type"`
	Status        int       `orm:"status"`
	CreatedAt     time.Time `orm:"created_at"`
}

// ExceptionList 异常单列表（status=0 表示全部）。
func ExceptionList(ctx context.Context, status, page, size int) ([]*ExceptionRow, int, error) {
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

	m := g.DB().Model("pay_exception").Where("shop_id", staff.ShopId)
	if status > 0 {
		m = m.Where("status", status)
	}

	total, err := m.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []*entityPayException
	err = m.Page(page, size).Order("created_at DESC").Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*ExceptionRow, 0, len(rows))
	for _, r := range rows {
		appStatusName := "已关闭"
		appStatus, err := g.DB().Model("appointment").
			Where("app_id", r.AppId).
			Value("status")
		if err == nil && !appStatus.IsNil() {
			if name, ok := consts.AppointmentStatusNames[appStatus.Int()]; ok {
				appStatusName = name
			}
		}
		list = append(list, &ExceptionRow{
			ExcId:         r.ExcId,
			AppId:         r.AppId,
			TransactionId: r.TransactionId,
			Amount:        r.Amount,
			ExcType:       r.ExcType,
			Status:        r.Status,
			CreatedAt:     r.CreatedAt,
			AppStatusName: appStatusName,
		})
	}
	return list, total, nil
}

// ExceptionProcessResult 异常单处理结果。
type ExceptionProcessResult struct {
	ExcId    string `json:"excId"`
	Status   int    `json:"status"`
	RefundId string `json:"refundId"`
	Message  string `json:"message"`
}

// ProcessException 处理异常单：发起全额退款（type=3），保持预约关闭、名额不恢复。
func ProcessException(ctx context.Context, excId string) (*ExceptionProcessResult, error) {
	staff := auth.StaffFrom(ctx)
	// 角色校验：店长(1)/客服(4)；前台/服务 → 40001
	if staff == nil || (staff.Role != consts.RoleManager && staff.Role != consts.RoleSupport) {
		return nil, biz.New(consts.ErrNoPermission)
	}

	// 异常单归属本店
	var exc entityPayException
	err := g.DB().Model("pay_exception").Where("exc_id", excId).Scan(&exc)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exc.ExcId == "" || exc.ShopId != staff.ShopId {
		return nil, biz.New(consts.ErrShopForbidden)
	}

	// 已处理不可重复处理 → 50010（条件更新防并发）
	r, err := g.DB().Model("pay_exception").
		Where("exc_id", excId).
		Where("status", consts.ExcStatusPending).
		Data(g.Map{"status": consts.ExcStatusRefunded}).
		Update()
	if err != nil {
		return nil, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return nil, biz.New(consts.ErrExcHandled)
	}

	// 全额退款单（type=3），模拟渠道立即打款成功
	refundId := "ref_" + guid.S()
	_, err = g.DB().Model("refund").Data(g.Map{
		"refund_id":   refundId,
		"app_id":      exc.AppId,
		"refund_type": consts.RefundTypeException,
		"amount":      exc.Amount,
		"status":      consts.RefundStatusSuccess,
		"refunded_at": time.Now(),
		"created_at":  time.Now(),
	}).Insert()
	if err != nil {
		return nil, err
	}
	// 关联退款单
	if _, err := g.DB().Model("pay_exception").Where("exc_id", excId).Data(g.Map{"refund_id": refundId}).Update(); err != nil {
		return nil, err
	}

	if err := WriteOperLog(ctx, staff.ShopId, staff.StaffId, exc.AppId, "EXC_REFUND", consts.ExcStatusPending, consts.ExcStatusRefunded, "支付异常单原路退款（type=3）"); err != nil {
		return nil, err
	}
	glog.Infof(ctx, "[exception] processed exc=%s app=%s refund=%s by=%s", excId, exc.AppId, refundId, staff.StaffId)
	return &ExceptionProcessResult{
		ExcId:    excId,
		Status:   consts.ExcStatusRefunded,
		RefundId: refundId,
		Message:  MsgExceptionProcessed,
	}, nil
}
