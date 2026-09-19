package platform

import (
	"testing"
	"time"
)

type fixedClock struct {
	value time.Time
}

func (c fixedClock) Now() time.Time {
	return c.value
}

func TestCurrentYearUsesAsiaShanghai(t *testing.T) {
	clock := fixedClock{value: time.Date(2025, time.December, 31, 16, 30, 0, 0, time.UTC)}
	if got := CurrentYear(clock); got != 2026 {
		t.Fatalf("CurrentYear() = %d, want 2026", got)
	}
}

func TestRealClockUsesConfiguredLocation(t *testing.T) {
	clock := NewRealClock(time.FixedZone("test", 9*60*60))
	if got := clock.Now().Location().String(); got != "test" {
		t.Fatalf("RealClock.Now() location = %q, want test", got)
	}
}
