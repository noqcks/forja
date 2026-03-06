package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadAndExistsRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := Default()
	cfg.Region = "us-east-1"
	cfg.CacheBucket = "forja-cache-test"
	cfg.PublishedAMI["amd64"] = "ami-amd64"
	cfg.PublishedAMI["arm64"] = "ami-arm64"

	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	exists, err := Exists()
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Fatal("expected config to exist after Save")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Region != cfg.Region || loaded.CacheBucket != cfg.CacheBucket {
		t.Fatalf("loaded config mismatch: %+v", loaded)
	}
	if loaded.Instances["amd64"] != "c7a.large" || loaded.Instances["arm64"] != "c7g.large" {
		t.Fatalf("expected default instances to be preserved, got %+v", loaded.Instances)
	}

	path, _ := ConfigPath()
	if path != filepath.Join(home, ".forja", "config.yaml") {
		t.Fatalf("unexpected config path: %s", path)
	}
}

func TestValidateRequiresCoreFields(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation to fail without region/cache bucket/amis")
	}

	cfg.Region = "us-east-1"
	cfg.CacheBucket = "forja-cache-test"
	cfg.PublishedAMI["amd64"] = "ami-amd64"
	cfg.PublishedAMI["arm64"] = "ami-arm64"
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() unexpected error = %v", err)
	}
}

func TestPricingCacheRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := &PricingCache{
		LastUpdated: time.Now().UTC().Truncate(time.Second),
		Prices: map[string]map[string]float64{
			"c7a.large": {"us-east-1": 0.07245},
		},
	}
	if err := SavePricingCache(cache); err != nil {
		t.Fatalf("SavePricingCache() error = %v", err)
	}

	loaded, err := LoadPricingCache()
	if err != nil {
		t.Fatalf("LoadPricingCache() error = %v", err)
	}
	if loaded.Prices["c7a.large"]["us-east-1"] != 0.07245 {
		t.Fatalf("unexpected price map: %+v", loaded.Prices)
	}

	path, _ := PricingPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected pricing file to exist: %v", err)
	}
}
