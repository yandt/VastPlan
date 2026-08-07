// Package profile validates a prebuilt Desktop profile before it is assigned.
package profile

import (
	"context"
	"errors"
	"fmt"

	appv1 "cdsoft.com.cn/VastPlan/contracts/schemas/app/v1"
)

type PluginRef = appv1.PluginRef
type Profile = appv1.Profile

// Catalog is backed by verified profile artifacts; it never accepts a manifest supplied by a Desktop.
type Catalog interface {
	SupportsDesktop(context.Context, PluginRef) (bool, error)
}

func Validate(ctx context.Context, p Profile, catalog Catalog) error {
	normalized, err := appv1.Validate(p)
	if err != nil {
		return fmt.Errorf("Desktop Profile 结构无效: %w", err)
	}
	p = normalized
	if catalog == nil {
		return errors.New("Desktop Profile 必须使用受信任插件目录")
	}
	for _, ref := range p.Plugins {
		ok, err := catalog.SupportsDesktop(ctx, ref)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("插件 %s 不兼容 Desktop", ref.ID)
		}
	}
	return nil
}

func Eligible(p Profile, desktopID string) bool {
	for _, id := range p.AssignedTo {
		if id == desktopID {
			return true
		}
	}
	return false
}
