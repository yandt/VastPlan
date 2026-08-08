package artifacttrust

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// ResolveExact tries ordered sources until one returns a trusted result.
// resolve must include every trust-domain check required before T is usable.
// ErrNotFound is the only error that advances to the next source; transport,
// permission, content, proof and lock failures remain fail-closed.
func ResolveExact[S any, T any](ctx context.Context, ref pluginv1.ArtifactRef, sources []S, sourceName func(S) string, resolve func(context.Context, S, pluginv1.ArtifactRef) (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		return zero, errors.New("制品源解析缺少 context")
	}
	if sourceName == nil || resolve == nil {
		return zero, errors.New("制品源解析协议未完整配置")
	}
	var notFound error
	for _, source := range sources {
		name := strings.TrimSpace(sourceName(source))
		if name == "" {
			return zero, errors.New("制品候选源未完整配置")
		}
		resolved, err := resolve(ctx, source, ref)
		if errors.Is(err, ErrNotFound) {
			notFound = errors.Join(notFound, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if err != nil {
			return zero, fmt.Errorf("制品源 %s 失败: %w", name, err)
		}
		return resolved, nil
	}
	if notFound != nil {
		return zero, fmt.Errorf("所有制品源均无精确引用 %s@%s/%s: %w", ref.PluginID, ref.Version, ref.Channel, notFound)
	}
	return zero, errors.New("没有可用制品源")
}
