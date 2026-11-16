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

// TestBookRejectsFinishedOccupancy 回归：已核销/服务完成(5/6/7/8)名额永久占用，
// 完成服务后同一时段不可重复预约（同宠物唯一）也不可超容量订入。
func TestBookRejectsFinishedOccupancy(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	defer cleanupSlot(t, ctx, slotId)

	finish := func(appId string) {
		t.Helper()
		_, err := g.DB().Model("appointment").Data(g.Map{"status": consts.StatusFinished}).Where("app_id", appId).Update()
		if err != nil {
			t.Fatalf("simulate finish: %v", err)
		}
	}
	// 1. 宠物 A 完成服务后重复预约同时段 → 40004（同宠物唯一，含核销后状态）
	r1, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: "occ_1_" + guid.S()})
	if err != nil {
		t.Fatalf("book1: %v", err)
	}
	finish(r1.AppId)
	_, err = Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: "occ_4_" + guid.S()})
	if err == nil {
		t.Fatal("same pet re-book after finished should be rejected")
	}
	if biz, ok := err.(*BizError); !ok || biz.Code != consts.ErrPetDuplicated {
		t.Fatalf("should be pet duplicated 40004, got %v", err)
	}

	// 2. 占满容量（含已完成订单）后换宠物订入 → 40003（容量校验统计 5/6/7/8）
	r2, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2002", SlotId: slotId, IdemKey: "occ_2_" + guid.S()})
	if err != nil {
		t.Fatalf("book2: %v", err)
	}
	finish(r2.AppId)
	r3, err := Book(ctx, &BookInput{UserId: "user_1002", PetId: "pet_2003", SlotId: slotId, IdemKey: "occ_3_" + guid.S()})
	if err != nil {
		t.Fatalf("book3: %v", err)
	}
	finish(r3.AppId)
	_, err = Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: "occ_5_" + guid.S()})
	if err == nil {
		t.Fatal("over-capacity booking with finished orders should be rejected")
	}
	if biz, ok := err.(*BizError); !ok || biz.Code != consts.ErrQuotaFull {
		t.Fatalf("should be quota full 40003, got %v", err)
	}
}
