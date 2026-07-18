// Package profile validates a prebuilt Runner profile before it is assigned.
package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}
type Profile struct {
	ID           string      `json:"id"`
	Revision     uint64      `json:"revision"`
	TenantID     string      `json:"tenantId"`
	Runtime      string      `json:"runtime"`
	Targets      []string    `json:"targets"`
	Distribution string      `json:"distribution"`
	AssignedTo   []string    `json:"assignedTo"`
	Plugins      []PluginRef `json:"plugins"`
}

// Catalog is backed by verified profile artifacts; it never accepts a manifest supplied by a Runner.
type Catalog interface {
	SupportsRunner(context.Context, PluginRef) (bool, error)
}

func Validate(ctx context.Context, p Profile, catalog Catalog) error {
	if p.ID == "" || p.Revision == 0 || p.TenantID == "" {
		return errors.New("Runner Profile 必须有 id、revision 和 tenant")
	}
	if p.Runtime != "runner" || p.Distribution != "self-update" {
		return errors.New("Runner Profile 只能使用 runner runtime 和 self-update 分发")
	}
	if len(p.Targets) == 0 || len(p.AssignedTo) == 0 || len(p.Plugins) == 0 {
		return errors.New("Runner Profile 必须声明 targets、assignedTo 和 plugins")
	}
	seen := map[string]bool{}
	for _, target := range p.Targets {
		if !strings.Contains(target, "/") || seen[target] {
			return fmt.Errorf("Runner target 无效或重复: %q", target)
		}
		seen[target] = true
	}
	if catalog == nil {
		return errors.New("Runner Profile 必须使用受信任插件目录")
	}
	seen = map[string]bool{}
	for _, ref := range p.Plugins {
		if ref.ID == "" || ref.Version == "" || seen[ref.ID] {
			return fmt.Errorf("Runner Profile 插件引用无效或重复: %q", ref.ID)
		}
		seen[ref.ID] = true
		ok, err := catalog.SupportsRunner(ctx, ref)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("插件 %s 不兼容 Runner", ref.ID)
		}
	}
	return nil
}
func Eligible(p Profile, runnerID string) bool {
	for _, id := range p.AssignedTo {
		if id == runnerID {
			return true
		}
	}
	return false
}
