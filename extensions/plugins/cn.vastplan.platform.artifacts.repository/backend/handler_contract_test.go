package main

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestRepositoryDescriptorMatchesInstalledHandlers(t *testing.T) {
	t.Parallel()
	var descriptor struct {
		Subcommands []struct {
			Name string `json:"name"`
		} `json:"subcommands"`
	}
	if err := json.Unmarshal(runtimeRepositoryDescriptor, &descriptor); err != nil {
		t.Fatalf("解析仓库 Descriptor: %v", err)
	}
	declared := make(map[string]struct{}, len(descriptor.Subcommands))
	for _, command := range descriptor.Subcommands {
		if command.Name == "" {
			t.Fatal("仓库 Descriptor 包含空操作名")
		}
		if _, duplicate := declared[command.Name]; duplicate {
			t.Fatalf("仓库 Descriptor 重复声明操作 %s", command.Name)
		}
		declared[command.Name] = struct{}{}
	}
	handlers := repositoryHandlers(serverConfig{}, nil, &runningRepositoryTransport{}, &dataPlaneLeaseRegistrar{})
	missing, unexpected := setDifference(declared, handlers), handlerDifference(handlers, declared)
	if len(missing) != 0 || len(unexpected) != 0 {
		t.Fatalf("仓库 Descriptor/Handler 不一致: missing=%v unexpected=%v", missing, unexpected)
	}
}

func setDifference[T any](expected map[string]struct{}, actual map[string]T) []string {
	missing := make([]string, 0)
	for name := range expected {
		if _, ok := actual[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func handlerDifference[T any](actual map[string]T, expected map[string]struct{}) []string {
	unexpected := make([]string, 0)
	for name := range actual {
		if _, ok := expected[name]; !ok {
			unexpected = append(unexpected, name)
		}
	}
	sort.Strings(unexpected)
	return unexpected
}
