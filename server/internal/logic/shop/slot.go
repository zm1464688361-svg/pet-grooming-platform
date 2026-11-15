// Package shop B 端门店业务（时段管理等）。
//
// 约束口径（AGENTS.md §4.4 / agentrules-B端 §2-3）：
//   - 接口先解析员工身份，再以 staff.shop_id 过滤数据（50001）；
//   - 角色级禁止：仅店长(1)可管理时段，越权 → 40001；
//   - 创建校验：结束晚于开始、开始晚于服务端当前时间、同门店不重叠（40002）；容量固定 3，忽略前端传值；
//   - 存在状态 1/2/3 预约时，禁止改时间、暂停、删除（50002）；
//   - 暂停仅无有效预约时可执行，恢复可随时；操作均写 oper_log（SLOT_*）。
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

// SlotView B 端时段视图（状态为推导值：待生效/暂停/进行中/已结束）。
type SlotView struct {
	SlotId      string    `json:"slotId"`
	ShopId      string    `json:"shopId"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
	MaxCapacity int       `json:"maxCapacity"` // 只读
	Status      int       `json:"status"`      // 推导状态
	Occupied    int       `json:"occupied"`    // 有效预约占用（status 1/2/3）
	CreatedBy   string    `json:"createdBy"`
}

type entityShopSlot struct {
	SlotId      string    `orm:"slot_id"`
	ShopId      string    `orm:"shop_id"`
	StartTime   time.Time `orm:"start_time"`
	EndTime     time.Time `orm:"end_time"`
	MaxCapacity int       `orm:"max_capacity"`
	Status      int       `orm:"status"`
	CreatedBy   string    `orm:"created_by"`
}

// requireRole 角色校验：仅指定角色可执行，越权 → 40001。
func requireRole(staff *auth.StaffIdentity, role int) error {
	if staff == nil || staff.Role != role {
		return biz.New(consts.ErrNoPermission)
	}
	return nil
}

// requireOwnSlot 校验时段归属本店（50001），并返回时段实体。
func requireOwnSlot(ctx context.Context, staff *auth.StaffIdentity, slotId string) (*entityShopSlot, error) {
	if staff == nil {
		return nil, biz.New(consts.ErrShopForbidden)
	}
	var s entityShopSlot
	err := g.DB().Model("time_slot").
		Where("slot_id", slotId).
		Where("shop_id", staff.ShopId). // shop_id 隔离红线
		Scan(&s)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if s.SlotId == "" {
		return nil, biz.New(consts.ErrShopForbidden) // 不存在或他店数据
	}
	return &s, nil
}

// validBookingCount 有效预约数（status 1/2/3）。
func validBookingCount(ctx context.Context, slotId string) (int, error) {
	return g.DB().Model("appointment").Ctx(ctx).
		Where("slot_id", slotId).
		WhereIn("status", consts.ValidStatuses).
		Count()
}

// CreateSlot 店长创建时段。
func CreateSlot(ctx context.Context, start, end time.Time) (string, error) {
	staff := auth.StaffFrom(ctx)
	if err := requireRole(staff, consts.RoleManager); err != nil {
		return "", err
	}
	// 时间校验（服务端时间，秒级）
	now := time.Now().Truncate(time.Second)
	if !end.Truncate(time.Second).After(start.Truncate(time.Second)) || !start.After(now) {
		return "", biz.New(consts.ErrSlotUnavailable)
	}
	// 同门店重叠校验
	n, err := g.DB().Model("time_slot").
		Where("shop_id", staff.ShopId).
		WhereLT("start_time", end).
		WhereGT("end_time", start).
		Count()
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", biz.New(consts.ErrSlotUnavailable) // 重叠
	}

	slotId := "slot_" + guid.S()
	_, err = g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      staff.ShopId,
		"start_time":   start,
		"end_time":     end,
		"max_capacity": consts.MaxCapacity, // 固定 3，忽略前端传值
		"status":       consts.SlotActive,
		"created_by":   staff.StaffId,
		"created_at":   time.Now(),
	}).Insert()
	if err != nil {
		return "", err
	}

	_, err = g.DB().Model("oper_log").Data(g.Map{
		"log_id":     "log_" + guid.S(),
		"shop_id":    staff.ShopId,
		"staff_id":   staff.StaffId,
		"app_id":     slotId, // 时段操作以 slot_id 记入 app_id 列
		"action":     "slot_create",
		"remark":     "发布时段",
		"created_at": time.Now(),
	}).Insert()
	if err != nil {
		return "", err
	}
	glog.Infof(ctx, "[slot] created slot=%s shop=%s by=%s", slotId, staff.ShopId, staff.StaffId)
	return slotId, nil
}

// PauseSlot 暂停时段（仅无有效预约时，50002）。
func PauseSlot(ctx context.Context, slotId string) error {
	staff := auth.StaffFrom(ctx)
	if err := requireRole(staff, consts.RoleManager); err != nil {
		return err
	}
	s, err := requireOwnSlot(ctx, staff, slotId)
	if err != nil {
		return err
	}
	if n, err := validBookingCount(ctx, slotId); err != nil {
		return err
	} else if n > 0 {
		return biz.New(consts.ErrSlotHasBooking)
	}
	return changeSlotStatus(ctx, staff, s, consts.SlotPaused, "slot_pause", "暂停时段")
}

// ResumeSlot 恢复时段（可随时）。
func ResumeSlot(ctx context.Context, slotId string) error {
	staff := auth.StaffFrom(ctx)
	if err := requireRole(staff, consts.RoleManager); err != nil {
		return err
	}
	s, err := requireOwnSlot(ctx, staff, slotId)
	if err != nil {
		return err
	}
	return changeSlotStatus(ctx, staff, s, consts.SlotActive, "slot_resume", "恢复时段")
}

// UpdateSlotTime 修改时段时间（存在有效预约 → 50002）。
func UpdateSlotTime(ctx context.Context, slotId string, start, end time.Time) error {
	staff := auth.StaffFrom(ctx)
	if err := requireRole(staff, consts.RoleManager); err != nil {
		return err
	}
	s, err := requireOwnSlot(ctx, staff, slotId)
	if err != nil {
		return err
	}
	if n, err := validBookingCount(ctx, slotId); err != nil {
		return err
	} else if n > 0 {
		return biz.New(consts.ErrSlotHasBooking)
	}

	now := time.Now().Truncate(time.Second)
	if !end.Truncate(time.Second).After(start.Truncate(time.Second)) || !start.After(now) {
		return biz.New(consts.ErrSlotUnavailable)
	}
	// 重叠校验（排除自身）
	n, err := g.DB().Model("time_slot").
		Where("shop_id", staff.ShopId).
		WhereNot("slot_id", slotId).
		WhereLT("start_time", end).
		WhereGT("end_time", start).
		Count()
	if err != nil {
		return err
	}
	if n > 0 {
		return biz.New(consts.ErrSlotUnavailable)
	}

	if _, err := g.DB().Model("time_slot").Where("slot_id", s.SlotId).Data(g.Map{
		"start_time": start,
		"end_time":   end,
		"updated_at": time.Now(),
	}).Update(); err != nil {
		return err
	}
	return writeSlotLog(ctx, staff, s.SlotId, "slot_update", "修改时段时间")
}

// DeleteSlot 删除时段（存在有效预约 → 50002）。
func DeleteSlot(ctx context.Context, slotId string) error {
	staff := auth.StaffFrom(ctx)
	if err := requireRole(staff, consts.RoleManager); err != nil {
		return err
	}
	s, err := requireOwnSlot(ctx, staff, slotId)
	if err != nil {
		return err
	}
	if n, err := validBookingCount(ctx, slotId); err != nil {
		return err
	} else if n > 0 {
		return biz.New(consts.ErrSlotHasBooking)
	}
	if _, err := g.DB().Model("time_slot").Where("slot_id", s.SlotId).Delete(); err != nil {
		return err
	}
	return writeSlotLog(ctx, staff, s.SlotId, "slot_delete", "删除时段")
}

// changeSlotStatus 条件更新时段状态并写日志。
func changeSlotStatus(ctx context.Context, staff *auth.StaffIdentity, s *entityShopSlot, to int, action, remark string) error {
	r, err := g.DB().Model("time_slot").
		Where("slot_id", s.SlotId).
		Where("status", s.Status).
		Data(g.Map{"status": to, "updated_at": time.Now()}).
		Update()
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return biz.New(consts.ErrStatusConflict)
	}
	return writeSlotLog(ctx, staff, s.SlotId, action, remark)
}

// writeSlotLog 时段操作日志。
func writeSlotLog(ctx context.Context, staff *auth.StaffIdentity, slotId, action, remark string) error {
	_, err := g.DB().Model("oper_log").Data(g.Map{
		"log_id":     "log_" + guid.S(),
		"shop_id":    staff.ShopId,
		"staff_id":   staff.StaffId,
		"app_id":     slotId,
		"action":     action,
		"remark":     remark,
		"created_at": time.Now(),
	}).Insert()
	return err
}

// ListShopSlots 本店时段列表（强制 shop_id 过滤 + DB 分页）。
// date 参数：YYYY-MM-DD 过滤当天时段，可为空。
func ListShopSlots(ctx context.Context, page, size int, date string) ([]*SlotView, error) {
	staff := auth.StaffFrom(ctx)
	if staff == nil {
		return nil, biz.New(consts.ErrShopForbidden)
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 20 // B 端默认 20 条
	}

	m := g.DB().Model("time_slot").Where("shop_id", staff.ShopId)
	if date != "" {
		m = m.WhereLike("start_time", date+"%")
	}

	var rows []*entityShopSlot
	err := m.Page(page, size).Order("start_time ASC").Scan(&rows)
	if err != nil {
		return nil, err
	}

	// 有效预约占用数
	occMap, err := slotOccupiedMap(ctx, staff.ShopId)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	list := make([]*SlotView, 0, len(rows))
	for _, r := range rows {
		list = append(list, &SlotView{
			SlotId:      r.SlotId,
			ShopId:      r.ShopId,
			StartTime:   r.StartTime,
			EndTime:     r.EndTime,
			MaxCapacity: r.MaxCapacity,
			Status:      viewSlotStatus(r, now),
			Occupied:    occMap[r.SlotId],
			CreatedBy:   r.CreatedBy,
		})
	}
	return list, nil
}

// slotOccupiedMap 各时段有效预约占用数。
func slotOccupiedMap(ctx context.Context, shopId string) (map[string]int, error) {
	res, err := g.DB().Model("appointment").
		Where("shop_id", shopId).
		WhereIn("status", consts.ValidStatuses).
		Fields("slot_id, COUNT(*) AS cnt").
		Group("slot_id").
		All()
	if err != nil {
		return nil, err
	}
	m := make(map[string]int, len(res))
	for _, r := range res {
		m[r["slot_id"].String()] = r["cnt"].Int()
	}
	return m, nil
}

// viewSlotStatus 时段展示状态推导（存储值仅 1/2；进行中/已结束由服务端时间推导）。
func viewSlotStatus(s *entityShopSlot, now time.Time) int {
	if s.Status == consts.SlotPaused {
		return consts.SlotPaused // 暂停保留展示
	}
	switch {
	case now.After(s.EndTime):
		return consts.SlotEnded
	case now.After(s.StartTime):
		return consts.SlotOngoing
	default:
		return consts.SlotActive
	}
}
