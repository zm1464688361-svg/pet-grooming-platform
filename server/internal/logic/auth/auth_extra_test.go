// Package auth 身份解析补充测试（分支覆盖）。
package auth

import (
	"context"
	"testing"

	"pet-grooming/internal/consts"
)

func TestResolveUserBranches(t *testing.T) {
	// 空用户 ID
	if u, err := ResolveUser(context.Background(), ""); err != nil || u != nil {
		t.Fatalf("empty userId should return nil,nil, got %v,%v", u, err)
	}
	// 存在的用户
	u, err := ResolveUser(context.Background(), "user_1001")
	if err != nil || u == nil {
		t.Fatalf("existing user: %v,%v", u, err)
	}
	// 不存在的用户
	if u, err := ResolveUser(context.Background(), "ghost_user"); err != nil || u != nil {
		t.Fatalf("ghost user should return nil,nil, got %v,%v", u, err)
	}
}

func TestResolveStaffBranches(t *testing.T) {
	// 空员工 ID
	if s, err := ResolveStaff(context.Background(), ""); err != nil || s != nil {
		t.Fatalf("empty staffId should return nil,nil, got %v,%v", s, err)
	}
	// 存在且在职
	s, err := ResolveStaff(context.Background(), "manager_01")
	if err != nil || s == nil {
		t.Fatalf("existing staff: %v,%v", s, err)
	}
	if s.ShopId != consts.ShopId || s.Role != consts.RoleManager {
		t.Fatalf("staff identity mismatch: %+v", s)
	}
	// 不存在
	if s, err := ResolveStaff(context.Background(), "ghost_staff"); err != nil || s != nil {
		t.Fatalf("ghost staff should return nil,nil, got %v,%v", s, err)
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := WithUser(context.Background(), "user_1001")
	if got := UserIdFrom(ctx); got != "user_1001" {
		t.Fatalf("UserIdFrom: %q", got)
	}
	if UserIdFrom(context.Background()) != "" {
		t.Fatal("empty ctx should yield empty userId")
	}
	ctx = WithStaff(ctx, &StaffIdentity{StaffId: "manager_01", ShopId: consts.ShopId, Role: consts.RoleManager})
	s := StaffFrom(ctx)
	if s == nil || s.StaffId != "manager_01" || s.ShopId != consts.ShopId || s.Role != consts.RoleManager {
		t.Fatalf("StaffFrom: %+v", s)
	}
	// 空上下文 → nil
	if StaffFrom(context.Background()) != nil {
		t.Fatal("empty ctx should yield nil staff")
	}
}
