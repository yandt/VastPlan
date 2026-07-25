package repositoryruntime

import (
	"errors"
	"fmt"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// DescribePlanning 返回精确制品的已验签 Manifest 投影，不读取或泄露包体、
// 物理对象路径、仓库凭证和存储 Provider 细节。
func (m *Manager) DescribePlanning(request pluginv1.ArtifactPlanningRequest) (pluginv1.ArtifactPlanningResponse, error) {
	if m == nil {
		return pluginv1.ArtifactPlanningResponse{}, errors.New("活动制品仓库不可用")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return pluginv1.ArtifactPlanningResponse{}, errors.New("活动制品仓库不可用")
	}
	items := make([]pluginv1.ArtifactPlanningDescriptor, 0, len(request.Refs))
	for _, ref := range request.Refs {
		entry, ok := m.active.catalog.Lookup(ref)
		if !ok {
			return pluginv1.ArtifactPlanningResponse{}, fmt.Errorf("制品规划描述不存在: %s@%s/%s", ref.PluginID, ref.Version, ref.Channel)
		}
		if err := m.active.catalog.RequireDelivery(ref); err != nil {
			return pluginv1.ArtifactPlanningResponse{}, err
		}
		artifact, _, err := m.active.signed.ReadMetadataWithAttestation(ref)
		if err != nil {
			return pluginv1.ArtifactPlanningResponse{}, fmt.Errorf("读取制品规划描述 %s: %w", ref.PluginID, err)
		}
		if artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.Channel != ref.Channel || artifact.SHA256 != entry.SHA256 {
			return pluginv1.ArtifactPlanningResponse{}, fmt.Errorf("制品规划描述与 Catalog 身份漂移: %s", ref.PluginID)
		}
		items = append(items, pluginv1.ArtifactPlanningDescriptor{
			Ref: ref, SHA256: artifact.SHA256, Publisher: entry.Publisher,
			Manifest: append([]byte(nil), artifact.Manifest...),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].Ref, items[j].Ref
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.Channel < right.Channel
	})
	response := pluginv1.ArtifactPlanningResponse{RepositoryRevision: m.active.catalog.Stats().Revision, Items: items}
	return pluginv1.ValidateArtifactPlanningResponse(response)
}
