// Package profile validates a prebuilt Runner profile before it is assigned.
package profile

import (
	"context"
	"errors"
	"fmt"

	appv1 "cdsoft.com.cn/VastPlan/contracts/schemas/app/v1"
)

type PluginRef = appv1.PluginRef
type Profile = appv1.Profile

// Catalog is backed by verified profile artifacts; it never accepts a manifest supplied by a Runner.
type Catalog interface {
	SupportsRunner(context.Context, PluginRef) (bool, error)
}

func Validate(ctx context.Context, p Profile, catalog Catalog) error {
	normalized, err := appv1.Validate(p)
	if err != nil {
		return fmt.Errorf("Runner Profile 结构无效: %w", err)
	}
	p = normalized
	if catalog == nil {
		return errors.New("Runner Profile 必须使用受信任插件目录")
	}
	for _, ref := range p.Plugins {
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
