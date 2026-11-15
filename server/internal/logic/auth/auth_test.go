package auth

import (
	"context"
	"testing"

	_ "pet-grooming/internal/boot"

	"pet-grooming/internal/consts"
)

// 测试依赖本地 MySQL 与 seed 数据（见 manifest/sql/seed.sql）。

func TestResolveUser(t *testing.T) {
	ctx := context.Background()

	u, err := ResolveUser(ctx, "user_1001")
	if err != nil {
		t.Fatalf("ResolveUser(user_1001) err = %v", err)
	}
	if u == nil || u.UserId != "user_1001" || u.Phone != "13800001001" {
		t.Fatalf("unexpected user: %+v", u)
	}

	u, err = ResolveUser(ctx, "user_not_exist")
	if err != nil {
		t.Fatalf("ResolveUser(not_exist) should not return db err: %v", err)
	}
	if u != nil {
		t.Fatalf("unknown user should be nil, got %+v", u)
	}
}

func TestResolveStaff(t *testing.T) {
	ctx := context.Background()

	// 客服 cs_01：角色 4，授权门店 shop_001
	s, err := ResolveStaff(ctx, "cs_01")
	if err != nil {
		t.Fatalf("ResolveStaff(cs_01) err = %v", err)
	}
	if s == nil || s.Role != consts.RoleSupport || s.ShopId != "shop_001" {
		t.Fatalf("unexpected staff: %+v", s)
	}

	// 店长
	s, _ = ResolveStaff(ctx, "manager_01")
	if s == nil || s.Role != consts.RoleManager {
		t.Fatalf("unexpected manager: %+v", s)
	}

	// 不存在员工
	s, err = ResolveStaff(ctx, "staff_not_exist")
	if err != nil {
		t.Fatalf("ResolveStaff(not_exist) should not return db err: %v", err)
	}
	if s != nil {
		t.Fatalf("unknown staff should be nil, got %+v", s)
	}
}
