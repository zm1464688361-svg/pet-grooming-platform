package user

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
)

func insertMyApp(t *testing.T, ctx context.Context, userId, petId string, status int) string {
	t.Helper()
	appId := "app_m_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":     appId,
		"user_id":    userId,
		"pet_id":     petId,
		"slot_id":    "slot_01",
		"shop_id":    consts.ShopId,
		"item_id":    consts.ItemId,
		"price":      consts.PriceCent,
		"status":     status,
		"created_at": time.Now(),
		"updated_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert appointment: %v", err)
	}
	return appId
}

func TestListMyAppointments(t *testing.T) {
	ctx := context.Background()
	a1 := insertMyApp(t, ctx, "user_1001", "pet_2001", consts.StatusPending)
	a2 := insertMyApp(t, ctx, "user_1001", "pet_2002", consts.StatusConfirmed)
	defer func() {
		_, _ = g.DB().Model("appointment").WhereIn("app_id", g.Slice{a1, a2}).Delete()
	}()

	// 分页
	list, total, err := ListMyAppointments(ctx, "user_1001", 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total < 2 {
		t.Fatalf("total should >=2, got %d", total)
	}
	found := 0
	for _, it := range list {
		if it.AppId == a1 || it.AppId == a2 {
			found++
		}
		if it.StatusName == "" {
			t.Fatal("status name should be mapped")
		}
	}
	if found != 2 {
		t.Fatalf("my orders missing, found=%d", found)
	}

	// user_id 隔离：user_1002 查不到 user_1001 的单
	_, total2, _ := ListMyAppointments(ctx, "user_1002", 1, 10)
	_ = total2
	list2, _, _ := ListMyAppointments(ctx, "user_1002", 1, 10)
	for _, it := range list2 {
		if it.AppId == a1 || it.AppId == a2 {
			t.Fatal("data isolation broken: other user's order returned")
		}
	}

	// 按 created_at 倒序
	if len(list) >= 2 {
		if list[0].CreatedAt.Before(list[1].CreatedAt) {
			t.Fatal("list should be ordered by created_at desc")
		}
	}
}

func TestMyAppointmentDetail(t *testing.T) {
	ctx := context.Background()
	appId := insertMyApp(t, ctx, "user_1001", "pet_2001", consts.StatusPending)
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", appId).Delete() }()

	d, err := GetMyAppointmentDetail(ctx, "user_1001", appId)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.AppId != appId || d.PetName != "豆豆" || d.StatusName != "待支付" {
		t.Fatalf("unexpected detail: %+v", d)
	}
	if d.Price != consts.PriceCent {
		t.Fatalf("price mismatch: %d", d.Price)
	}

	// 越权访问 → 40001
	if _, err := GetMyAppointmentDetail(ctx, "user_1002", appId); err == nil || err.Error() != "40001" {
		t.Fatalf("other user detail should fail 40001, got %v", err)
	}

	// 不存在 → 40001
	if _, err := GetMyAppointmentDetail(ctx, "user_1001", "app_none"); err == nil || err.Error() != "40001" {
		t.Fatalf("missing detail should fail 40001, got %v", err)
	}
}
