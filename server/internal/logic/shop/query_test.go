package shop

import (
	"context"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
)

func insertQueryApp(t *testing.T, ctx context.Context, slotId, userId, petId string, status int, remark string) string {
	t.Helper()
	appId := "app_q_" + guid.S()
	_, err := g.DB().Model("appointment").Data(g.Map{
		"app_id":      appId,
		"user_id":     userId,
		"pet_id":      petId,
		"slot_id":     slotId,
		"shop_id":     consts.ShopId,
		"item_id":     consts.ItemId,
		"price":       consts.PriceCent,
		"status":      status,
		"user_remark": remark,
		"created_at":  time.Now(),
		"updated_at":  time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert appointment: %v", err)
	}
	return appId
}

func TestAppointmentList(t *testing.T) {
	ctx := staffCtx(consts.RoleFront, consts.ShopId)
	app1 := insertQueryApp(t, ctx, "slot_01", "user_1001", "pet_2001", consts.StatusConfirmed, "狗狗怕水")
	defer func() { _, _ = g.DB().Model("appointment").Where("app_id", app1).Delete() }()

	// 按订单号检索
	list, total, err := QueryAppointments(ctx, "", 0, "", app1, 1, 20)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total < 1 {
		t.Fatal("should find by appId keyword")
	}

	// 按宠物名模糊检索
	list, total, err = QueryAppointments(ctx, "", 0, "豆豆", "", 1, 20)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	found := false
	for _, it := range list {
		if it.AppId == app1 {
			found = true
		}
		if it.UserPhone == "" {
			t.Fatal("user phone should be joined")
		}
		if it.PetName == "" {
			t.Fatal("pet name should be joined")
		}
		if it.StartTime.IsZero() {
			t.Fatal("slot time should be joined")
		}
	}
	if !found || total < 1 {
		t.Fatalf("should find pet by fuzzy name, total=%d", total)
	}

	// 按状态过滤
	_, totalC, err := QueryAppointments(ctx, "", consts.StatusConfirmed, "", "", 1, 20)
	if err != nil || totalC < 1 {
		t.Fatalf("status filter: %v %d", err, totalC)
	}

	// 创建时间倒序
	listAll, _, _ := QueryAppointments(ctx, "", 0, "", "", 1, 20)
	if len(listAll) >= 2 && listAll[0].CreatedAt.Before(listAll[1].CreatedAt) {
		t.Fatal("list should be created_at desc")
	}
}

func TestAppointmentListShopIsolation(t *testing.T) {
	ctx := staffCtx(consts.RoleFront, "shop_002")
	_, total, err := QueryAppointments(ctx, "", 0, "", "", 1, 20)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 0 {
		t.Fatalf("shop_002 should see no data, got %d", total)
	}
}

func TestPetHistory(t *testing.T) {
	ctx := staffCtx(consts.RoleFront, consts.ShopId)
	slotId := "slot_b_" + guid.S()
	slotId2 := "slot_b_" + guid.S()
	for _, sid := range []string{slotId, slotId2} {
		_, err := g.DB().Model("time_slot").Data(g.Map{
			"slot_id":      sid,
			"shop_id":      consts.ShopId,
			"start_time":   time.Now().Add(72 * time.Hour),
			"end_time":     time.Now().Add(73 * time.Hour),
			"max_capacity": consts.MaxCapacity,
			"status":       consts.SlotActive,
			"created_by":   "manager_01",
			"created_at":   time.Now(),
		}).Insert()
		if err != nil {
			t.Fatal(err)
		}
	}
	app1, app2 := "", ""
	defer func() {
		if app1 != "" || app2 != "" {
			_, _ = g.DB().Model("appointment").WhereIn("app_id", g.Slice{app1, app2}).Delete()
		}
		_, _ = g.DB().Model("time_slot").WhereIn("slot_id", g.Slice{slotId, slotId2}).Delete()
	}()
	app1 = insertQueryApp(t, ctx, slotId, "user_1001", "pet_2001", consts.StatusPending, "")
	app2 = insertQueryApp(t, ctx, slotId2, "user_1001", "pet_2001", consts.StatusConfirmed, "")

	list, total, err := PetHistory(ctx, "pet_2001", 1, 20)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if total < 2 {
		t.Fatalf("history should >=2, got %d", total)
	}
	found := 0
	for _, it := range list {
		if it.AppId == app1 || it.AppId == app2 {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("history items missing, found=%d", found)
	}

	// 店铺隔离：其他店员工查不到
	ctx2 := staffCtx(consts.RoleManager, "shop_002")
	_, total2, _ := PetHistory(ctx2, "pet_2001", 1, 20)
	if total2 != 0 {
		t.Fatalf("cross-shop history should be 0, got %d", total2)
	}
}
