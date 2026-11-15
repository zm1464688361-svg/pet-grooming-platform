// Package shop B 端预约查询与宠物历史（全角色可用）。
//
// 约束口径（agentrules-B端 §4）：
//   - 日期 → 状态 → 手机号/宠物名/订单号模糊检索；DB 层强制分页（默认 20）；创建时间倒序；
//   - 查询强制注入 staff.shop_id（跨店 → 无数据）；shop_remark 仅 B 端可见，C 端接口不返回；
//   - 宠物历史只读，不参与状态变更。
package shop

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/auth"
	"pet-grooming/internal/model/biz"
)

// AppointmentRow B 端预约列表项。
type AppointmentRow struct {
	AppId      string    `json:"appId"`
	UserPhone  string    `json:"userPhone"`
	PetName    string    `json:"petName"`
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
	Price      int64     `json:"price"`
	Status     int       `json:"status"`
	StatusName string    `json:"statusName"`
	UserRemark string    `json:"userRemark"` // 双向可见
	ShopRemark string    `json:"shopRemark"` // 门店内部备注（C 端不返回）
	CreatedAt  time.Time `json:"createdAt"`
}

type entityShopApp struct {
	AppId      string    `orm:"app_id"`
	Phone      string    `orm:"phone"`
	PetName    string    `orm:"pet_name"`
	StartTime  time.Time `orm:"start_time"`
	EndTime    time.Time `orm:"end_time"`
	Price      int64     `orm:"price"`
	Status     int       `orm:"status"`
	UserRemark string    `orm:"user_remark"`
	ShopRemark string    `orm:"shop_remark"`
	CreatedAt  time.Time `orm:"created_at"`
}

// QueryAppointments 预约查询（全角色）。
// date=YYYY-MM-DD；status=0 表示不过滤；keyword 匹配手机号/宠物名/订单号（模糊）。
func QueryAppointments(ctx context.Context, date string, status int, petName, keyword string, page, size int) ([]*AppointmentRow, int, error) {
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

	m := g.DB().Model("appointment a").
		LeftJoin("user u", "u.user_id=a.user_id").
		LeftJoin("pet p", "p.pet_id=a.pet_id").
		LeftJoin("time_slot s", "s.slot_id=a.slot_id").
		Where("a.shop_id", staff.ShopId) // shop_id 隔离红线

	if date != "" {
		m = m.WhereLike("s.start_time", date+"%")
	}
	if status > 0 {
		m = m.Where("a.status", status)
	}
	if petName != "" {
		m = m.WhereLike("p.pet_name", "%"+petName+"%")
	}
	if keyword != "" {
		m = m.Where(
			"u.phone LIKE ? OR p.pet_name LIKE ? OR a.app_id LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%",
		)
	}

	total, err := m.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []*entityShopApp
	err = m.Page(page, size).
		Fields("a.app_id,a.price,a.status,a.user_remark,a.shop_remark,a.created_at,u.phone,p.pet_name,s.start_time,s.end_time").
		Order("a.created_at DESC").
		Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*AppointmentRow, 0, len(rows))
	for _, r := range rows {
		list = append(list, &AppointmentRow{
			AppId:      r.AppId,
			UserPhone:  r.Phone,
			PetName:    r.PetName,
			StartTime:  r.StartTime,
			EndTime:    r.EndTime,
			Price:      r.Price,
			Status:     r.Status,
			StatusName: consts.AppointmentStatusNames[r.Status],
			UserRemark: r.UserRemark,
			ShopRemark: r.ShopRemark,
			CreatedAt:  r.CreatedAt,
		})
	}
	return list, total, nil
}

// PetHistory 宠物在本店的历史预约（只读）。
func PetHistory(ctx context.Context, petId string, page, size int) ([]*AppointmentRow, int, error) {
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

	m := g.DB().Model("appointment a").
		LeftJoin("user u", "u.user_id=a.user_id").
		LeftJoin("pet p", "p.pet_id=a.pet_id").
		LeftJoin("time_slot s", "s.slot_id=a.slot_id").
		Where("a.shop_id", staff.ShopId). // 仅本店历史
		Where("a.pet_id", petId)

	total, err := m.Count()
	if err != nil {
		return nil, 0, err
	}

	var rows []*entityShopApp
	err = m.Page(page, size).
		Fields("a.app_id,a.price,a.status,a.user_remark,a.shop_remark,a.created_at,u.phone,p.pet_name,s.start_time,s.end_time").
		Order("a.created_at DESC").
		Scan(&rows)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*AppointmentRow, 0, len(rows))
	for _, r := range rows {
		list = append(list, &AppointmentRow{
			AppId:      r.AppId,
			UserPhone:  r.Phone,
			PetName:    r.PetName,
			StartTime:  r.StartTime,
			EndTime:    r.EndTime,
			Price:      r.Price,
			Status:     r.Status,
			StatusName: consts.AppointmentStatusNames[r.Status],
			UserRemark: r.UserRemark,
			ShopRemark: r.ShopRemark,
			CreatedAt:  r.CreatedAt,
		})
	}
	return list, total, nil
}
