package cost

import (
	"math"
	"testing"
)

func TestEstimateAppliesMinimumBillingWindow(t *testing.T) {
	t.Parallel()

	got := Estimate(30, 0.072)
	want := 60 * 0.072 / 3600
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("Estimate(30, 0.072) = %v, want %v", got, want)
	}
}

func TestEstimateUsesActualRuntimeAboveMinimum(t *testing.T) {
	t.Parallel()

	got := Estimate(600, 0.072)
	want := 600 * 0.072 / 3600
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("Estimate(600, 0.072) = %v, want %v", got, want)
	}
}
