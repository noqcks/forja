package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	dirName         = ".forja"
	configFileName  = "config.yaml"
	pricingFileName = "pricing.json"
)

type Config struct {
	Provider            string            `yaml:"provider"`
	Region              string            `yaml:"region"`
	DefaultPlatform     string            `yaml:"default_platform"`
	Instances           map[string]string `yaml:"instances"`
	Registry            string            `yaml:"registry,omitempty"`
	CacheBucket         string            `yaml:"cache_bucket"`
	CacheTTLDays        int               `yaml:"cache_ttl_days"`
	SelfDestructMinutes int               `yaml:"self_destruct_minutes"`
	PublishedAMI        map[string]string `yaml:"published_ami,omitempty"`
	Resources           Resources         `yaml:"resources"`
}

type Resources struct {
	AccountID           string            `yaml:"account_id"`
	SecurityGroupID     string            `yaml:"security_group_id"`
	SecurityGroupName   string            `yaml:"security_group_name"`
	IAMRoleName         string            `yaml:"iam_role_name"`
	IAMRoleARN          string            `yaml:"iam_role_arn"`
	InstanceProfileName string            `yaml:"instance_profile_name"`
	InstanceProfileARN  string            `yaml:"instance_profile_arn"`
	DefaultVPCID        string            `yaml:"default_vpc_id"`
	DefaultSubnetIDs    []string          `yaml:"default_subnet_ids"`
	LaunchTemplates     map[string]string `yaml:"launch_templates"`
	AMI                 map[string]string `yaml:"ami"`
}

type PricingCache struct {
	LastUpdated time.Time                     `json:"last_updated"`
	Prices      map[string]map[string]float64 `json:"prices"`
}

func Default() *Config {
	return &Config{
		Provider:            "aws",
		DefaultPlatform:     "linux/amd64",
		Instances:           map[string]string{"amd64": "c7a.8xlarge", "arm64": "c7g.8xlarge"},
		CacheTTLDays:        14,
		SelfDestructMinutes: 60,
		PublishedAMI:        map[string]string{},
		Resources: Resources{
			LaunchTemplates: map[string]string{},
			AMI:             map[string]string{},
		},
	}
}

func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	normalize(cfg)
	return cfg, nil
}

func Save(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	normalize(cfg)
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Exists() (bool, error) {
	path, err := ConfigPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	if cfg.Provider == "" {
		return errors.New("provider is required")
	}
	if cfg.Region == "" {
		return errors.New("region is required")
	}
	if cfg.CacheBucket == "" {
		return errors.New("cache_bucket is required")
	}
	if len(cfg.Instances) == 0 {
		return errors.New("instances is required")
	}
	return nil
}

func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return dirName
	}
	return filepath.Join(home, dirName)
}

func ConfigPath() (string, error) {
	return filepath.Join(ConfigDir(), configFileName), nil
}

func PricingPath() (string, error) {
	return filepath.Join(ConfigDir(), pricingFileName), nil
}

func LoadPricingCache() (*PricingCache, error) {
	path, err := PricingPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cache PricingCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse pricing cache: %w", err)
	}
	if cache.Prices == nil {
		cache.Prices = map[string]map[string]float64{}
	}
	return &cache, nil
}

func SavePricingCache(cache *PricingCache) error {
	if cache == nil {
		return errors.New("pricing cache is nil")
	}
	if cache.Prices == nil {
		cache.Prices = map[string]map[string]float64{}
	}
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pricing cache: %w", err)
	}
	path, err := PricingPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func normalize(cfg *Config) {
	if cfg.Provider == "" {
		cfg.Provider = "aws"
	}
	if cfg.DefaultPlatform == "" {
		cfg.DefaultPlatform = "linux/amd64"
	}
	if cfg.CacheTTLDays == 0 {
		cfg.CacheTTLDays = 14
	}
	if cfg.SelfDestructMinutes == 0 {
		cfg.SelfDestructMinutes = 60
	}
	if cfg.Instances == nil {
		cfg.Instances = map[string]string{}
	}
	if cfg.PublishedAMI == nil {
		cfg.PublishedAMI = map[string]string{}
	}
	if cfg.Resources.LaunchTemplates == nil {
		cfg.Resources.LaunchTemplates = map[string]string{}
	}
	if cfg.Resources.AMI == nil {
		cfg.Resources.AMI = map[string]string{}
	}
	cfg.Provider = strings.ToLower(cfg.Provider)
}
