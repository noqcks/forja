package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/noqcks/forja/internal/config"
)

var pricingRegionNames = map[string]string{
	"us-east-1":      "US East (N. Virginia)",
	"us-east-2":      "US East (Ohio)",
	"us-west-1":      "US West (N. California)",
	"us-west-2":      "US West (Oregon)",
	"ca-central-1":   "Canada (Central)",
	"eu-west-1":      "EU (Ireland)",
	"eu-central-1":   "EU (Frankfurt)",
	"ap-southeast-1": "Asia Pacific (Singapore)",
}

func (p *Provider) InstancePrice(ctx context.Context, region string, instanceType string) (float64, error) {
	cache, err := config.LoadPricingCache()
	if err == nil && time.Since(cache.LastUpdated) < 30*24*time.Hour {
		if price := cache.Prices[instanceType][region]; price > 0 {
			return price, nil
		}
	}
	price, err := p.lookupPrice(ctx, region, instanceType)
	if err != nil {
		return 0, err
	}
	cache = &config.PricingCache{
		LastUpdated: time.Now().UTC(),
		Prices:      map[string]map[string]float64{},
	}
	if existing, loadErr := config.LoadPricingCache(); loadErr == nil && existing.Prices != nil {
		cache = existing
		cache.LastUpdated = time.Now().UTC()
	}
	if cache.Prices[instanceType] == nil {
		cache.Prices[instanceType] = map[string]float64{}
	}
	cache.Prices[instanceType][region] = price
	_ = config.SavePricingCache(cache)
	return price, nil
}

func (p *Provider) lookupPrice(ctx context.Context, region string, instanceType string) (float64, error) {
	location := pricingRegionNames[region]
	if location == "" {
		return 0, fmt.Errorf("unsupported pricing lookup region %s", region)
	}
	out, err := p.pricing.GetProducts(ctx, &pricing.GetProductsInput{
		ServiceCode: sdkaws.String("AmazonEC2"),
		Filters: []pricingtypes.Filter{
			{Type: pricingtypes.FilterTypeTermMatch, Field: sdkaws.String("instanceType"), Value: sdkaws.String(instanceType)},
			{Type: pricingtypes.FilterTypeTermMatch, Field: sdkaws.String("location"), Value: sdkaws.String(location)},
			{Type: pricingtypes.FilterTypeTermMatch, Field: sdkaws.String("operatingSystem"), Value: sdkaws.String("Linux")},
			{Type: pricingtypes.FilterTypeTermMatch, Field: sdkaws.String("preInstalledSw"), Value: sdkaws.String("NA")},
			{Type: pricingtypes.FilterTypeTermMatch, Field: sdkaws.String("tenancy"), Value: sdkaws.String("Shared")},
			{Type: pricingtypes.FilterTypeTermMatch, Field: sdkaws.String("capacitystatus"), Value: sdkaws.String("Used")},
		},
		MaxResults: sdkaws.Int32(1),
	})
	if err != nil {
		return 0, fmt.Errorf("pricing get products: %w", err)
	}
	if len(out.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing data found for %s in %s", instanceType, region)
	}
	var product struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit map[string]string `json:"pricePerUnit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}
	if err := json.Unmarshal([]byte(out.PriceList[0]), &product); err != nil {
		return 0, fmt.Errorf("parse pricing response: %w", err)
	}
	for _, term := range product.Terms.OnDemand {
		for _, dim := range term.PriceDimensions {
			if value := strings.TrimSpace(dim.PricePerUnit["USD"]); value != "" {
				price, parseErr := strconv.ParseFloat(value, 64)
				if parseErr != nil {
					return 0, fmt.Errorf("parse hourly price: %w", parseErr)
				}
				return price, nil
			}
		}
	}
	return 0, fmt.Errorf("no on-demand usd price found for %s in %s", instanceType, region)
}
