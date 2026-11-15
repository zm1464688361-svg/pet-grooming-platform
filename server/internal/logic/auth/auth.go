// Package auth 身份解析：C 端用户 / B 端员工（简化登录态，见 docs/plans/assumptions.md 假设 3）。
package auth

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/model/entity"
)

// ctxKey Context 键类型。
type ctxKey string

const (
	ctxKeyUserId  ctxKey = "userId"
	ctxKeyStaffId ctxKey = "staffId"
	ctxKeyShopId  ctxKey = "shopId"
	ctxKeyRole    ctxKey = "role"
)

// StaffIdentity B 端员工身份。
type StaffIdentity struct {
	StaffId   string
	ShopId    string
	StaffName string
	Role      int
}

// ResolveUser 解析 C 端用户；不存在返回 nil（无 DB 错误）。
func ResolveUser(ctx context.Context, userId string) (*entity.User, error) {
	if userId == "" {
		return nil, nil
	}
	var u *entity.User
	err := g.DB().Model("user").Where("user_id", userId).Scan(&u)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ResolveStaff 解析 B 端员工（仅在职）；不存在返回 nil（无 DB 错误）。
func ResolveStaff(ctx context.Context, staffId string) (*StaffIdentity, error) {
	if staffId == "" {
		return nil, nil
	}
	var s *entity.ShopStaff
	err := g.DB().Model("shop_staff").
		Where("staff_id", staffId).
		Where("status", consts.StaffStatusActive).
		Scan(&s)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	return &StaffIdentity{
		StaffId:   s.StaffId,
		ShopId:    s.ShopId,
		StaffName: s.StaffName,
		Role:      s.Role,
	}, nil
}

// WithUser 将用户身份写入 Context。
func WithUser(ctx context.Context, userId string) context.Context {
	return context.WithValue(ctx, ctxKeyUserId, userId)
}

// WithStaff 将员工身份写入 Context。
func WithStaff(ctx context.Context, s *StaffIdentity) context.Context {
	ctx = context.WithValue(ctx, ctxKeyStaffId, s.StaffId)
	ctx = context.WithValue(ctx, ctxKeyShopId, s.ShopId)
	ctx = context.WithValue(ctx, ctxKeyRole, s.Role)
	return ctx
}

// UserIdFrom 从 Context 取当前用户 ID。
func UserIdFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserId).(string)
	return v
}

// StaffFrom 从 Context 取当前员工身份。
func StaffFrom(ctx context.Context) *StaffIdentity {
	sid, _ := ctx.Value(ctxKeyStaffId).(string)
	shopId, _ := ctx.Value(ctxKeyShopId).(string)
	role, _ := ctx.Value(ctxKeyRole).(int)
	if sid == "" {
		return nil
	}
	return &StaffIdentity{StaffId: sid, ShopId: shopId, Role: role}
}
