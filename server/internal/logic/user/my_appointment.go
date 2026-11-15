// Package user C 端"我的预约"查询（列表/详情）。
// 数据隔离红线：所有查询强制 user_id 条件，禁止越权访问他人订单。
package user

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/model/biz"
)

// MyAppointmentListItem 列表项。
type MyAppointmentListItem struct {
	AppId      string    `json:"appId"`
	PetName    string    `json:"petName"`
	ItemName   string    `json:"itemName"`
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
	Price      int64     `json:"price"`
	Status     int       `json:"status"`
	StatusName string    `json:"statusName"`
	CreatedAt  time.Time `json:"createdAt"`
}

// MyAppointmentDetail 详情。
type MyAppointmentDetail struct {
	AppId        string     `json:"appId"`
	PetName      string     `json:"petName"`
	ItemName     string     `json:"itemName"`
	ShopName     string     `json:"shopName"`
	StartTime    time.Time  `json:"startTime"`
	EndTime      time.Time  `json:"endTime"`
	Price        int64      `json:"price"`
	Status       int        `json:"status"`
	StatusName   string     `json:"statusName"`
	UserRemark   string     `json:"userRemark"`
	CancelReason string     `json:"cancelReason"`
	AuditResult  *int       `json:"auditResult"`
	LockExpireAt *time.Time `json:"lockExpireAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// ListMyAppointments 分页查询当前用户预约（created_at 倒序）。
func ListMyAppointments(ctx context.Context, userId string, page, size int) ([]MyAppointmentListItem, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 10
	}

	m := g.DB().Model("appointment a").
		LeftJoin("pet p", "p.pet_id=a.pet_id").
		LeftJoin("service_item i", "i.item_id=a.item_id").
		LeftJoin("time_slot s", "s.slot_id=a.slot_id").
		Where("a.user_id", userId) // 数据隔离红线

	total, err := m.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []*myAppRow
	err = m.Page(page, size).
		Fields("a.app_id,a.status,a.price,a.created_at,p.pet_name,i.item_name,s.start_time,s.end_time").
		Order("a.created_at DESC").
		Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	list := make([]MyAppointmentListItem, 0, len(rows))
	for _, r := range rows {
		list = append(list, MyAppointmentListItem{
			AppId:      r.AppId,
			PetName:    r.PetName,
			ItemName:   r.ItemName,
			StartTime:  r.StartTime,
			EndTime:    r.EndTime,
			Price:      r.Price,
			Status:     r.Status,
			StatusName: consts.AppointmentStatusNames[r.Status],
			CreatedAt:  r.CreatedAt,
		})
	}
	return list, total, nil
}

// GetMyAppointmentDetail 查询预约详情（越权/不存在 → 40001）。
func GetMyAppointmentDetail(ctx context.Context, userId, appId string) (*MyAppointmentDetail, error) {
	var row *myAppDetailRow
	err := g.DB().Model("appointment a").
		LeftJoin("pet p", "p.pet_id=a.pet_id").
		LeftJoin("service_item i", "i.item_id=a.item_id").
		LeftJoin("shop sh", "sh.shop_id=a.shop_id").
		LeftJoin("time_slot s", "s.slot_id=a.slot_id").
		Where("a.app_id", appId).
		Fields("a.*,p.pet_name,i.item_name,sh.shop_name,s.start_time,s.end_time").
		Scan(&row)
	if err != nil {
		return nil, err
	}
	if row == nil || row.UserId != userId { // 越权与不存在一律 40001
		return nil, biz.New(consts.ErrNoPermission)
	}
	return &MyAppointmentDetail{
		AppId:        row.AppId,
		PetName:      row.PetName,
		ItemName:     row.ItemName,
		ShopName:     row.ShopName,
		StartTime:    row.StartTime,
		EndTime:      row.EndTime,
		Price:        row.Price,
		Status:       row.Status,
		StatusName:   consts.AppointmentStatusNames[row.Status],
		UserRemark:   row.UserRemark,
		CancelReason: row.CancelReason,
		AuditResult:  row.AuditResult,
		LockExpireAt: row.LockExpireAt,
		CreatedAt:    row.CreatedAt,
	}, nil
}

// ORM 行实体
type myAppRow struct {
	AppId     string    `orm:"app_id"`
	PetName   string    `orm:"pet_name"`
	ItemName  string    `orm:"item_name"`
	StartTime time.Time `orm:"start_time"`
	EndTime   time.Time `orm:"end_time"`
	Price     int64     `orm:"price"`
	Status    int       `orm:"status"`
	CreatedAt time.Time `orm:"created_at"`
}

type myAppDetailRow struct {
	AppId        string     `orm:"app_id"`
	UserId       string     `orm:"user_id"`
	PetName      string     `orm:"pet_name"`
	ItemName     string     `orm:"item_name"`
	ShopName     string     `orm:"shop_name"`
	StartTime    time.Time  `orm:"start_time"`
	EndTime      time.Time  `orm:"end_time"`
	Price        int64      `orm:"price"`
	Status       int        `orm:"status"`
	UserRemark   string     `orm:"user_remark"`
	CancelReason string     `orm:"cancel_reason"`
	AuditResult  *int       `orm:"audit_result"`
	LockExpireAt *time.Time `orm:"lock_expire_at"`
	CreatedAt    time.Time  `orm:"created_at"`
}
