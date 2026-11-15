package quota

import (
	"context"
	"fmt"
	"sync"
	"testing"

	_ "pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"
)

// 测试依赖本地 Redis（docker compose up -d redis）与 MySQL seed 数据。

func newTestSlotId() string {
	return "slot_test_" + guid.S()
}

func cleanKeys(t *testing.T, slotId string) {
	t.Helper()
	ctx := context.Background()
	_, err := g.Redis().Do(ctx, "DEL", countKey(slotId), petsKey(slotId))
	if err != nil {
		t.Fatalf("clean keys: %v", err)
	}
}

func TestOccupyConcurrent(t *testing.T) {
	ctx := context.Background()
	slotId := newTestSlotId()
	defer cleanKeys(t, slotId)

	const capacity = 3
	const goroutines = 10

	var (
		mu        sync.Mutex
		successes int
	)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			petId := fmt.Sprintf("pet_%d", n)
			_, err := Occupy(ctx, slotId, petId, capacity)
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if successes != capacity {
		t.Fatalf("concurrent occupy: want %d successes, got %d", capacity, successes)
	}

	remaining, err := Remaining(ctx, slotId, capacity)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining should be 0, got %d", remaining)
	}
}

func TestOccupyPetDuplicated(t *testing.T) {
	ctx := context.Background()
	slotId := newTestSlotId()
	defer cleanKeys(t, slotId)

	if _, err := Occupy(ctx, slotId, "pet_a", 3); err != nil {
		t.Fatalf("first occupy: %v", err)
	}
	_, err := Occupy(ctx, slotId, "pet_a", 3)
	if err == nil || err.Error() != reasonPetDuplicated {
		t.Fatalf("second occupy should fail with pet duplicated, got %v", err)
	}
}

func TestOccupyQuotaFull(t *testing.T) {
	ctx := context.Background()
	slotId := newTestSlotId()
	defer cleanKeys(t, slotId)

	for _, p := range []string{"p1", "p2", "p3"} {
		if _, err := Occupy(ctx, slotId, p, 3); err != nil {
			t.Fatalf("occupy %s: %v", p, err)
		}
	}
	_, err := Occupy(ctx, slotId, "p4", 3)
	if err == nil || err.Error() != reasonQuotaFull {
		t.Fatalf("occupy beyond capacity should fail with quota full, got %v", err)
	}
}

func TestReleaseIdempotent(t *testing.T) {
	ctx := context.Background()
	slotId := newTestSlotId()
	defer cleanKeys(t, slotId)

	if _, err := Occupy(ctx, slotId, "pet_a", 3); err != nil {
		t.Fatalf("occupy: %v", err)
	}

	// 首次释放成功
	if err := Release(ctx, slotId, "pet_a"); err != nil {
		t.Fatalf("first release: %v", err)
	}

	// 重复释放不产生负数（先 SISMEMBER 判断）
	if err := Release(ctx, slotId, "pet_a"); err != nil {
		t.Fatalf("second release must be tolerated: %v", err)
	}

	remaining, err := Remaining(ctx, slotId, 3)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if remaining != 3 {
		t.Fatalf("remaining after double release should be 3, got %d", remaining)
	}
}

func TestRebuildQuota(t *testing.T) {
	ctx := context.Background()
	slotId := newTestSlotId()
	defer cleanKeys(t, slotId)

	// 模拟计数被污染
	_, _ = g.Redis().Do(ctx, "SET", countKey(slotId), 99)

	// slot_01 在 seed 中为未来时段、无预约；对账后计数应为 0
	if err := RebuildQuota(ctx, "slot_01"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	remaining, err := Remaining(ctx, "slot_01", 3)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if remaining != 3 {
		t.Fatalf("remaining after rebuild should be 3, got %d", remaining)
	}
}
