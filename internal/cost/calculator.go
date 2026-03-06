package cost

import "math"

func Estimate(runtimeSeconds float64, hourlyPrice float64) float64 {
	billableSeconds := math.Max(runtimeSeconds, 60)
	return billableSeconds * hourlyPrice / 3600
}
