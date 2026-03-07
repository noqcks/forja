package release

import (
	"sort"
	"strings"
)

func AWSAMI(region string, arch string) string {
	region = strings.TrimSpace(region)
	arch = strings.TrimSpace(arch)
	if region == "" || arch == "" {
		return ""
	}
	amis, ok := awsAMIs[region]
	if !ok {
		return ""
	}
	return amis[arch]
}

func AWSRegions() []string {
	regions := make([]string, 0, len(awsAMIs))
	for r := range awsAMIs {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	return regions
}

func AWSAMIsForRegion(region string) (map[string]string, bool) {
	region = strings.TrimSpace(region)
	if region == "" {
		return nil, false
	}
	amis, ok := awsAMIs[region]
	if !ok {
		return nil, false
	}
	result := make(map[string]string, len(amis))
	for arch, ami := range amis {
		result[arch] = ami
	}
	return result, true
}
