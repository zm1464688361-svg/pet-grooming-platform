package user

import (
	"context"
	"sync"
	"testing"
	"time"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"

	"pet-grooming/internal/consts"
	"pet-grooming/internal/logic/quota"
)

// createTestSlot 创建未来测试时段。
func createTestSlot(t *testing.T, ctx context.Context, startOffset time.Duration) string {
	t.Helper()
	slotId := "slot_t_" + guid.S()
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      consts.ShopId,
		"start_time":   time.Now().Add(startOffset),
		"end_time":     time.Now().Add(startOffset + time.Hour),
		"max_capacity": consts.MaxCapacity,
		"status":       consts.SlotActive,
		"created_by":   "manager_01",
		"created_at":   time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("create slot: %v", err)
	}
	return slotId
}

// cleanupSlot 清理测试时段的所有预约与 Redis key。
func cleanupSlot(t *testing.T, ctx context.Context, slotId string) {
	t.Helper()
	var petIds []string
	_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Array("pet_id", &petIds)
	for _, p := range petIds {
		_ = quota.Release(ctx, slotId, p)
	}
	_, _ = g.Redis().Do(ctx, "DEL", quota.CountKey(slotId), "slot:pets:"+slotId)
	_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Delete()
	_, _ = g.DB().Model("time_slot").Where("slot_id", slotId).Delete()
}

func TestBookSuccess(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	defer cleanupSlot(t, ctx, slotId)

	res, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	if res.AppId == "" || res.Status != consts.StatusPending {
		t.Fatalf("unexpected result: %+v", res)
	}

	// 锁定 5 分钟
	expire := time.Until(res.LockExpireAt)
	if expire < 4*time.Minute || expire > 5*time.Minute+10*time.Second {
		t.Fatalf("lock_expire_at should be ~5min later, got %v", expire)
	}

	// 名额已占用（时序：订单落库后才占名额）
	remaining, _ := quota.Remaining(ctx, slotId, consts.MaxCapacity)
	if remaining != consts.MaxCapacity-1 {
		t.Fatalf("remaining should be %d, got %d", consts.MaxCapacity-1, remaining)
	}
}

func TestBookConcurrentLastQuota(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	defer cleanupSlot(t, ctx, slotId)

	// 4 人并发抢同一名额周期（容量 3），恰好 1 人失败 40003
	pets := []string{"pet_2001", "pet_2003", "pet_2005", "pet_2002"} // 分属 3 个用户
	users := []string{"user_1001", "user_1002", "user_1003", "user_1001"}

	var (
		mu    sync.Mutex
		fail3 int
		okCnt int
	)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := Book(ctx, &BookInput{UserId: users[n], PetId: pets[n], SlotId: slotId})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if err.Error() == "40003" {
					fail3++
				}
			} else {
				okCnt++
			}
		}(i)
	}
	wg.Wait()

	if okCnt != 3 || fail3 != 1 {
		t.Fatalf("want 3 ok / 1 fail(40003), got ok=%d fail=%d", okCnt, fail3)
	}
}

func TestBookPetDuplicated(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	defer cleanupSlot(t, ctx, slotId)

	if _, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId}); err != nil {
		t.Fatalf("first book: %v", err)
	}
	_, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	if err == nil || err.Error() != "40004" {
		t.Fatalf("duplicate pet book should fail 40004, got %v", err)
	}
}

func TestBookPetNotOwner(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	defer cleanupSlot(t, ctx, slotId)

	_, err := Book(ctx, &BookInput{UserId: "user_1002", PetId: "pet_2001", SlotId: slotId})
	if err == nil || err.Error() != "40001" {
		t.Fatalf("other user's pet should fail 40001, got %v", err)
	}
}

func TestBookDisabledPet(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	defer cleanupSlot(t, ctx, slotId)

	_, err := Book(ctx, &BookInput{UserId: "user_1002", PetId: "pet_2004", SlotId: slotId})
	if err == nil || err.Error() != "40001" {
		t.Fatalf("disabled pet should fail 40001, got %v", err)
	}
}

func TestBookSlotStarted(t *testing.T) {
	ctx := context.Background()

	// 已开始的时段（status=1，start_time < now）
	slotId := "slot_t_" + guid.S()
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      consts.ShopId,
		"start_time":   time.Now().Add(-time.Hour),
		"end_time":     time.Now().Add(-30 * time.Minute),
		"max_capacity": consts.MaxCapacity,
		"status":       consts.SlotActive,
		"created_by":   "manager_01",
		"created_at":   time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer cleanupSlot(t, ctx, slotId)

	_, err = Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId})
	if err == nil || err.Error() != "40002" {
		t.Fatalf("started slot should fail 40002, got %v", err)
	}
}

func TestBookIdempotent(t *testing.T) {
	ctx := context.Background()
	slotId := createTestSlot(t, ctx, 3*time.Hour)
	defer cleanupSlot(t, ctx, slotId)

	idemKey := "idem_" + guid.S()
	defer func() { _, _ = g.Redis().Do(ctx, "DEL", idemKeyOf("user_1001", idemKey)) }()

	if _, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: idemKey}); err != nil {
		t.Fatalf("first book: %v", err)
	}
	_, err := Book(ctx, &BookInput{UserId: "user_1001", PetId: "pet_2001", SlotId: slotId, IdemKey: idemKey})
	if err == nil || err.Error() != "40007" {
		t.Fatalf("idempotent hit should fail 40007, got %v", err)
	}
}
