// Package user 预约下单。
//
// 时序红线（AGENTS.md §4.1）：DB 事务提交成功（status=1 订单落库）→ 才调用 Redis Lua 占用名额。
// 禁止先扣名额再建单；禁止支付成功才扣名额。
package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
	"pet-grooming/internal/model/biz"
)

// BizError 业务校验错误（别名，统一使用 model/biz）。
type BizError = biz.Error

// NewBiz 构造业务错误。
func NewBiz(code int) error {
	return biz.New(code)
}

// BookInput 预约入参。
type BookInput struct {
	UserId     string // 来自登录态，不信任前端
	PetId      string
	SlotId     string
	IdemKey    string // 幂等键（可选）
	UserRemark string // 用户备注（双向可见）
}

// BookResult 预约结果。
type BookResult struct {
	AppId        string    `json:"appId"`
	Status       int       `json:"status"`
	LockExpireAt time.Time `json:"lockExpireAt"`
	Message      string    `json:"message"`
}

// idemKeyOf 幂等键。
func idemKeyOf(userId, key string) string {
	return fmt.Sprintf("book:idem:%s:%s", userId, key)
}

// Book 创建预约订单（时序：事务提交成功后占用名额）。
func Book(ctx context.Context, in *BookInput) (*BookResult, error) {
	// 1. 幂等：SETNX 10 分钟，重复提交返回 40007
	if in.IdemKey != "" {
		res, err := g.Redis().Do(ctx, "SET", idemKeyOf(in.UserId, in.IdemKey), "1", "NX", "EX", 600)
		if err != nil {
			return nil, err
		}
		if res.String() != "OK" {
			return nil, NewBiz(consts.ErrIdempotentHit)
		}
	}

	// 2. 宠物校验：归属当前用户且启用（40001）
	var pet entityPet
	err := g.DB().Model("pet").
		Where("pet_id", in.PetId).
		Where("user_id", in.UserId). // 数据隔离红线
		Where("status", consts.PetStatusActive).
		Scan(&pet)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if pet.PetId == "" {
		return nil, NewBiz(consts.ErrNoPermission)
	}

	// 3. 时段准入：本店发布、可预约、未开始（40002）
	var slot entitySlot
	err = g.DB().Model("time_slot").
		Where("slot_id", in.SlotId).
		Where("shop_id", consts.ShopId).
		Scan(&slot)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if slot.SlotId == "" || slot.Status != consts.SlotActive || !slot.StartTime.After(time.Now()) {
		return nil, NewBiz(consts.ErrSlotUnavailable)
	}

	// 4. DB 事务：行锁串行化同时段预约 → 容量/唯一校验 → 插入 status=1 订单
	lockExpireAt := time.Now().Add(consts.LockMinutes * time.Minute)
	appId := "app_" + guid.S()

	txErr := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		// 4.1 锁定时段行，串行化同时段并发下单
		_, err := tx.Model("time_slot").Ctx(ctx).Where("slot_id", in.SlotId).LockUpdate().One()
		if err != nil {
			return err
		}

		// 4.2 容量校验（占用名额状态 1/2/3 + 5/6/7/8，红线：核销后名额永久占用）
		cnt, err := tx.Model("appointment").Ctx(ctx).
			Where("slot_id", in.SlotId).
			WhereIn("status", consts.OccupiedStatuses).
			Count()
		if err != nil {
			return err
		}
		if cnt >= consts.MaxCapacity {
			return NewBiz(consts.ErrQuotaFull)
		}

		// 4.3 同宠物唯一校验（占用名额状态全集；uk_pet_slot 兜底）
		dup, err := tx.Model("appointment").Ctx(ctx).
			Where("pet_id", in.PetId).
			Where("slot_id", in.SlotId).
			WhereIn("status", consts.OccupiedStatuses).
			Count()
		if err != nil {
			return err
		}
		if dup > 0 {
			return NewBiz(consts.ErrPetDuplicated)
		}

		// 4.4 插入待支付订单（lock_expire_at = now + 5min，服务端写入）
		_, err = tx.Model("appointment").Ctx(ctx).Data(g.Map{
			"app_id":         appId,
			"user_id":        in.UserId,
			"pet_id":         in.PetId,
			"slot_id":        in.SlotId,
			"shop_id":        consts.ShopId,
			"item_id":        consts.ItemId,
			"price":          consts.PriceCent,
			"status":         consts.StatusPending,
			"user_remark":    in.UserRemark,
			"lock_expire_at": lockExpireAt,
			"created_at":     time.Now(),
			"updated_at":     time.Now(),
		}).Insert()
		return err
	})

	// 4.5 业务校验失败（事务已回滚）
	if txErr != nil {
		if biz, ok := txErr.(*BizError); ok {
			return nil, &BizError{Code: biz.Code}
		}
		return nil, txErr
	}

	// 5. 事务提交成功 → 占用名额（时序红线：DB 先行，Redis 后行）
	occupied, occErr := quota.Occupy(ctx, in.SlotId, in.PetId, consts.MaxCapacity)
	if occErr != nil || !occupied {
		// Redis 与 DB 不一致：业务订单已成立（DB 为真值源），告警 + 对账重建，不篡改业务数据
		glog.Errorf(ctx, "[quota] ALARM book=%s slot=%s occupy failed err=%v, rebuild", appId, in.SlotId, occErr)
		if rErr := quota.RebuildQuota(ctx, in.SlotId); rErr != nil {
			glog.Errorf(ctx, "[quota] ALARM rebuild failed: %v", rErr)
		}
	}

	glog.Infof(ctx, "[book] created appId=%s user=%s pet=%s slot=%s", appId, in.UserId, in.PetId, in.SlotId)
	return &BookResult{
		AppId:        appId,
		Status:       consts.StatusPending,
		LockExpireAt: lockExpireAt,
		Message:      consts.MsgBookSuccess,
	}, nil
}
