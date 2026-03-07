package cli

import (
	"math"
	"testing"
	"time"

	"github.com/noqcks/forja/internal/cloud"
)

func TestEstimateBuildCostUsesPerInstanceLaunchTimes(t *testing.T) {
	t.Parallel()

	buildStart := time.Date(2026, 3, 6, 12, 0, 10, 0, time.UTC)
	buildEnd := buildStart.Add(75 * time.Second)
	builders := []launchedBuilder{
		{
			instance: &cloud.BuilderInstance{LaunchTime: buildStart.Add(-10 * time.Second)},
			price:    0.07245,
		},
		{
			instance: &cloud.BuilderInstance{LaunchTime: buildStart.Add(-25 * time.Second)},
			price:    0.06800,
		},
	}

	got := estimateBuildCost(builders, buildStart, buildEnd)
	want := (85 * 0.07245 / 3600) + (100 * 0.06800 / 3600)
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("estimateBuildCost(...) = %v, want %v", got, want)
	}
}

func TestEstimateBuildCostFallsBackToBuildWindowWithoutLaunchTime(t *testing.T) {
	t.Parallel()

	buildStart := time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	buildEnd := buildStart.Add(45 * time.Second)
	builders := []launchedBuilder{
		{
			instance: &cloud.BuilderInstance{},
			price:    0.07245,
		},
	}

	got := estimateBuildCost(builders, buildStart, buildEnd)
	want := 60 * 0.07245 / 3600
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("estimateBuildCost(...) = %v, want %v", got, want)
	}
}
