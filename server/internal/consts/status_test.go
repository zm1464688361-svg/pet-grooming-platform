package consts

import "testing"

func TestStatusEnums(t *testing.T) {
	// 状态枚举与约束一致（禁止魔数漂移）
	if StatusPending != 1 || StatusConfirmed != 2 || StatusCancelAudit != 3 ||
		StatusCancelled != 4 || StatusVerified != 5 || StatusInService != 6 ||
		StatusFinished != 7 || StatusNoShow != 8 || StatusTimeoutClosed != 9 {
		t.Fatalf("appointment status enum mismatch")
	}

	// 有效状态 = 1/2/3
	for _, s := range []int{1, 2, 3} {
		if !IsValidStatus(s) {
			t.Fatalf("status %d should be valid", s)
		}
	}
	for _, s := range []int{4, 5, 6, 7, 8, 9} {
		if IsValidStatus(s) {
			t.Fatalf("status %d should not be valid", s)
		}
	}

	// 终态 = 4~9
	for _, s := range []int{4, 5, 6, 7, 8, 9} {
		if !IsFinalStatus(s) {
			t.Fatalf("status %d should be final", s)
		}
	}
	for _, s := range []int{1, 2, 3} {
		if IsFinalStatus(s) {
			t.Fatalf("status %d should not be final", s)
		}
	}

	// 状态文案全覆盖
	if len(AppointmentStatusNames) != 9 {
		t.Fatalf("status names should cover 9 statuses, got %d", len(AppointmentStatusNames))
	}
}

func TestErrorMessages(t *testing.T) {
	// C 端 40001~40011、B 端 50001~50010 文案全覆盖
	for code := 40001; code <= 40011; code++ {
		if _, ok := ErrorMessages[code]; !ok {
			t.Fatalf("missing C-side error message for %d", code)
		}
	}
	for code := 50001; code <= 50010; code++ {
		if _, ok := ErrorMessages[code]; !ok {
			t.Fatalf("missing B-side error message for %d", code)
		}
	}
}
