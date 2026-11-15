// Package user C 端业务逻辑：时段查询、宠物查询。
// 时段准入三重过滤：shop 匹配 + status=可预约 + start_time > now（服务端时间）。
package user

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
)

// SlotView C 端可约时段视图。
type SlotView struct {
	SlotId    string    `json:"slotId"`
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	Remaining int       `json:"remaining"` // 剩余名额
	IsFull    bool      `json:"isFull"`    // 是否已满
}

// PetView C 端宠物视图（预置数据，仅查看）。
type PetView struct {
	PetId   string `json:"petId"`
	PetName string `json:"petName"`
	PetType string `json:"petType"`
	UserId  string `json:"userId"`
	Status  int    `json:"status"`
}

// ListBookableSlots 查询可预约时段列表（升序），附剩余名额。
func ListBookableSlots(ctx context.Context) ([]SlotView, error) {
	var slots []entitySlot
	err := g.DB().Model("time_slot").
		Where("shop_id", consts.ShopId).
		Where("status", consts.SlotActive).
		WhereGT("start_time", time.Now()). // 服务端时间，秒级判定
		Order("start_time ASC").
		Scan(&slots)
	if err != nil {
		return nil, err
	}

	views := make([]SlotView, 0, len(slots))
	for _, s := range slots {
		remaining, err := remainingForSlot(ctx, s.SlotId)
		if err != nil {
			return nil, err
		}
		views = append(views, SlotView{
			SlotId:    s.SlotId,
			StartTime: s.StartTime,
			EndTime:   s.EndTime,
			Remaining: remaining,
			IsFull:    remaining <= 0,
		})
	}
	return views, nil
}

// remainingForSlot 查询时段剩余名额；Redis 计数缺失时先对账重建（DB 为真值源）。
func remainingForSlot(ctx context.Context, slotId string) (int, error) {
	exists, err := g.Redis().Do(ctx, "EXISTS", quota.CountKey(slotId))
	if err != nil {
		return 0, err
	}
	if exists.Int() == 0 {
		if err := quota.RebuildQuota(ctx, slotId); err != nil {
			return 0, err
		}
	}
	return quota.Remaining(ctx, slotId, consts.MaxCapacity)
}

// ListPetsForUser 查询当前用户名下启用宠物（强制 user_id 隔离，禁止越权）。
func ListPetsForUser(ctx context.Context, userId string) ([]PetView, error) {
	var pets []entityPet
	err := g.DB().Model("pet").
		Where("user_id", userId). // 数据隔离红线
		Where("status", consts.PetStatusActive).
		Order("created_at ASC").
		Scan(&pets)
	if err != nil {
		return nil, err
	}

	views := make([]PetView, 0, len(pets))
	for _, p := range pets {
		views = append(views, PetView{
			PetId:   p.PetId,
			PetName: p.PetName,
			PetType: p.PetType,
			UserId:  p.UserId,
			Status:  p.Status,
		})
	}
	return views, nil
}

// ItemView 服务项目信息（预置）。
type ItemView struct {
	ItemId    string `json:"itemId"`
	ItemName  string `json:"itemName"`
	PriceCent int64  `json:"priceCent"`
}

// GetItemInfo 查询服务项目（预置数据 item_wash_001，9900 分）。
func GetItemInfo(ctx context.Context) (*ItemView, error) {
	var item entityItem
	err := g.DB().Model("service_item").
		Where("item_id", consts.ItemId).
		Where("status", 1).
		Scan(&item)
	if err != nil {
		return nil, err
	}
	if item.ItemId == "" {
		return nil, nil
	}
	return &ItemView{
		ItemId:    item.ItemId,
		ItemName:  item.ItemName,
		PriceCent: item.PriceCent,
	}, nil
}

// 内部 ORM 实体（与表字段对应）
type entitySlot struct {
	SlotId      string    `orm:"slot_id"`
	ShopId      string    `orm:"shop_id"`
	StartTime   time.Time `orm:"start_time"`
	EndTime     time.Time `orm:"end_time"`
	MaxCapacity int       `orm:"max_capacity"`
	Status      int       `orm:"status"`
}

type entityPet struct {
	PetId   string `orm:"pet_id"`
	UserId  string `orm:"user_id"`
	PetName string `orm:"pet_name"`
	PetType string `orm:"pet_type"`
	Status  int    `orm:"status"`
}

type entityItem struct {
	ItemId    string `orm:"item_id"`
	ShopId    string `orm:"shop_id"`
	ItemName  string `orm:"item_name"`
	PriceCent int64  `orm:"price_cent"`
	Status    int    `orm:"status"`
}
