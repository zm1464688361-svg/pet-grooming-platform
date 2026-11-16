// Package quota 名额占用与释放（Redis Lua 原子操作）。
//
// 红线约束（AGENTS.md §4.1）：
//   - 占用脚本仅在 DB 事务提交成功（status=1 订单落库）后调用；
//   - 释放仅限 4 个合法场景（待支付超时/待支付主动取消/≥2h 自动取消/审批同意），
//     统一走 Release，禁止其他代码直接操作 Redis 计数；
//   - 释放脚本先 SISMEMBER 判断占用，避免重复释放把计数减成负数；
//   - MySQL 是真值源，Redis 不一致时告警并可用 RebuildQuota 对账重建。
package quota

import (
	"context"
	"errors"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"

	"pet-grooming/internal/consts"
)

const (
	keyCountPrefix = "slot:count:" // 计数 key：slot:count:{slotId}
	keyPetsPrefix  = "slot:pets:"  // 宠物集合 key：slot:pets:{slotId}
)

func countKey(slotId string) string { return keyCountPrefix + slotId }
func petsKey(slotId string) string  { return keyPetsPrefix + slotId }

// CountKey 导出计数 key（供对账判断使用）。
func CountKey(slotId string) string { return countKey(slotId) }

// Lua 返回原因
const (
	reasonOK            = "OK"
	reasonPetDuplicated = "PET_DUPLICATED"
	reasonQuotaFull     = "QUOTA_FULL"
	reasonNotOccupied   = "NOT_OCCUPIED"
	reasonNegativeCount = "NEGATIVE_COUNT"
)

// 错误定义
var (
	ErrPetDuplicated = errors.New(reasonPetDuplicated)
	ErrQuotaFull     = errors.New(reasonQuotaFull)
)

// occupyScript 占用名额：原子判断宠物是否已预约、名额是否已满。
// 返回 {1,'OK'} / {0,'PET_DUPLICATED'} / {0,'QUOTA_FULL'}
var occupyScript = `
local petExists = redis.call('SISMEMBER', KEYS[2], ARGV[1])
if petExists == 1 then return {0, '` + reasonPetDuplicated + `'} end
local cnt = tonumber(redis.call('GET', KEYS[1]) or '0')
if cnt >= tonumber(ARGV[2]) then return {0, '` + reasonQuotaFull + `'} end
redis.call('INCR', KEYS[1])
redis.call('SADD', KEYS[2], ARGV[1])
return {1, '` + reasonOK + `'}
`

// releaseScript 释放名额：先 SISMEMBER 判断占用；以 SREM 实际移除结果决定 DECR，
// 防止并发重复释放把计数减成负数；出现负数即视为异常并告警。
// 返回 {1,'OK'} / {0,'NOT_OCCUPIED'} / {0,'NEGATIVE_COUNT'}
var releaseScript = `
local ok = redis.call('SISMEMBER', KEYS[2], ARGV[1])
if ok == 0 then return {0, '` + reasonNotOccupied + `'} end
local removed = redis.call('SREM', KEYS[2], ARGV[1])
if removed == 0 then return {0, '` + reasonNotOccupied + `'} end
local cnt = redis.call('DECR', KEYS[1])
if cnt < 0 then
  redis.call('SET', KEYS[1], 0)
  return {0, '` + reasonNegativeCount + `'}
end
return {1, '` + reasonOK + `'}
`

// evalRedis 运行 Lua 脚本并返回 [status, reason]。
func evalRedis(ctx context.Context, script string, slotId, petId string, capacity int) (int, string, error) {
	args := []interface{}{script, 2, countKey(slotId), petsKey(slotId), petId}
	if capacity > 0 {
		args = append(args, capacity)
	}
	res, err := g.Redis().Do(ctx, "EVAL", args...)
	if err != nil {
		return 0, "", err
	}
	arr := res.Slice()
	if len(arr) != 2 {
		return 0, "", errors.New("redis lua 返回结构异常")
	}
	status := g.NewVar(arr[0]).Int()
	reason := g.NewVar(arr[1]).String()
	return status, reason, nil
}

// Occupy 占用名额。返回是否占用成功；ErrPetDuplicated / ErrQuotaFull 表示业务拒绝。
func Occupy(ctx context.Context, slotId, petId string, capacity int) (bool, error) {
	status, reason, err := evalRedis(ctx, occupyScript, slotId, petId, capacity)
	if err != nil {
		return false, err
	}
	switch reason {
	case reasonOK:
		glog.Debugf(ctx, "[quota] occupy slot=%s pet=%s ok, capacity=%d", slotId, petId, capacity)
		return status == 1, nil
	case reasonPetDuplicated:
		return false, ErrPetDuplicated
	case reasonQuotaFull:
		return false, ErrQuotaFull
	default:
		return false, errors.New("occupy failed: " + reason)
	}
}

// Release 释放名额（唯一释放入口）。
// 重复释放（未占用）幂等容忍；计数出现负数视为异常并告警。
func Release(ctx context.Context, slotId, petId string) error {
	_, reason, err := evalRedis(ctx, releaseScript, slotId, petId, 0)
	if err != nil {
		return err
	}
	switch reason {
	case reasonOK:
		glog.Infof(ctx, "[quota] release slot=%s pet=%s ok", slotId, petId)
		return nil
	case reasonNotOccupied:
		// 未占用（可能已被释放），幂等容忍，只记日志
		glog.Infof(ctx, "[quota] release slot=%s pet=%s not occupied, skip", slotId, petId)
		return nil
	case reasonNegativeCount:
		glog.Errorf(ctx, "[quota] ALARM slot=%s pet=%s negative count detected, rebuilt to 0", slotId, petId)
		return errors.New("quota count negative, alarmed")
	default:
		return errors.New("release failed: " + reason)
	}
}

// Remaining 查询剩余名额。
func Remaining(ctx context.Context, slotId string, capacity int) (int, error) {
	res, err := g.Redis().Do(ctx, "GET", countKey(slotId))
	if err != nil {
		return 0, err
	}
	cnt := res.Int()
	return capacity - cnt, nil
}

// RebuildQuota 对账重建：以 MySQL 有效预约（状态 1/2/3）为准重建计数与宠物集合。
func RebuildQuota(ctx context.Context, slotId string) error {
	// 对账重建统计占用名额状态全集（1/2/3 + 5/6/7/8，红线：核销后名额永久占用）
	vals, err := g.DB().Model("appointment").
		Where("slot_id", slotId).
		WhereIn("status", consts.OccupiedStatuses).
		Array("pet_id")
	if err != nil {
		return err
	}
	petIds := make([]string, 0, len(vals))
	for _, v := range vals {
		petIds = append(petIds, v.String())
	}

	if _, err := g.Redis().Do(ctx, "DEL", countKey(slotId), petsKey(slotId)); err != nil {
		return err
	}
	if len(petIds) > 0 {
		args := make([]interface{}, 0, len(petIds)+2)
		args = append(args, petsKey(slotId))
		for _, p := range petIds {
			args = append(args, p)
		}
		if _, err := g.Redis().Do(ctx, "SADD", args...); err != nil {
			return err
		}
		if _, err := g.Redis().Do(ctx, "SET", countKey(slotId), len(petIds)); err != nil {
			return err
		}
	}
	glog.Infof(ctx, "[quota] rebuild slot=%s from db, valid=%d", slotId, len(petIds))
	return nil
}
