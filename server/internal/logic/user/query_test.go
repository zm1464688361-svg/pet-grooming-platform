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

// 测试依赖 seed 数据（slot_01~03 均为未来时段；pet_2004 停用）。

func TestListBookableSlots(t *testing.T) {
	ctx := context.Background()

	slots, err := ListBookableSlots(ctx)
	if err != nil {
		t.Fatalf("ListBookableSlots: %v", err)
	}
	if len(slots) < 3 {
		t.Fatalf("seed 中 3 个未来时段均应可约，got %d", len(slots))
	}
	for _, s := range slots {
		// 剩余名额 = 容量 - 占用名额状态(1/2/3/5/6/7/8)的订单数（Redis 为缓存，允许演示数据占用）
		occ, err := g.DB().Model("appointment").
			Where("slot_id", s.SlotId).
			WhereIn("status", consts.OccupiedStatuses).
			Count()
		if err != nil {
			t.Fatalf("count appointments: %v", err)
		}
		expect := consts.MaxCapacity - occ
		if expect < 0 {
			expect = 0
		}
		if s.Remaining != expect {
			t.Fatalf("slot %s remaining should be %d (occupied %d), got %d", s.SlotId, expect, occ, s.Remaining)
		}
	}

	// 按开始时间升序
	for i := 1; i < len(slots); i++ {
		if slots[i-1].StartTime.After(slots[i].StartTime) {
			t.Fatalf("时段应按开始时间升序")
		}
	}
}

func TestListBookableSlotsExcludesStarted(t *testing.T) {
	ctx := context.Background()

	// 插入已开始时段（status=1 但 start_time < now）与已结束时段 → 均不返回
	slotId := "slot_expired_" + guid.S()
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      consts.ShopId,
		"start_time":   time.Now().Add(-1 * time.Hour),
		"end_time":     time.Now().Add(-30 * time.Minute),
		"max_capacity": consts.MaxCapacity,
		"status":       consts.SlotActive,
		"created_by":   "manager_01",
		"created_at":   time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert started slot: %v", err)
	}
	defer func() {
		_, _ = g.DB().Model("time_slot").Where("slot_id", slotId).Delete()
	}()

	slots, err := ListBookableSlots(ctx)
	if err != nil {
		t.Fatalf("ListBookableSlots: %v", err)
	}
	for _, s := range slots {
		if s.SlotId == slotId {
			t.Fatalf("已开始时段不应返回")
		}
	}
}

func TestListPetsForUser(t *testing.T) {
	ctx := context.Background()

	pets, err := ListPetsForUser(ctx, "user_1001")
	if err != nil {
		t.Fatalf("ListPetsForUser: %v", err)
	}
	if len(pets) != 2 {
		t.Fatalf("user_1001 应有 2 只启用宠物，got %d", len(pets))
	}
	for _, p := range pets {
		if p.UserId != "user_1001" {
			t.Fatalf("数据越权：返回了他人宠物 %s", p.PetId)
		}
		if p.Status != consts.PetStatusActive {
			t.Fatalf("停用宠物不应返回")
		}
	}

	// 停用宠物（pet_2004 属 user_1002）不返回
	pets2, _ := ListPetsForUser(ctx, "user_1002")
	for _, p := range pets2 {
		if p.PetId == "pet_2004" {
			t.Fatalf("停用宠物不应返回")
		}
	}

	// 空用户
	pets3, err := ListPetsForUser(ctx, "user_none")
	if err != nil || len(pets3) != 0 {
		t.Fatalf("不存在用户应返回空列表, err=%v len=%d", err, len(pets3))
	}
}
