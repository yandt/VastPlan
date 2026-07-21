package repositoryruntime

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/garbagecollection"
)

var (
	quotaIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,79}$`)
	quotaNamespacePattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,159}$`)
	quotaPublisherPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,159}$`)
	quotaChannelPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

type QuotaLimit struct {
	MaxArtifacts int   `json:"maxArtifacts,omitempty"`
	MaxBytes     int64 `json:"maxBytes,omitempty"`
}

type QuotaRule struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Channel   string `json:"channel,omitempty"`
	QuotaLimit
}

type QuotaPolicy struct {
	QuotaLimit
	Rules []QuotaRule `json:"rules,omitempty"`
}

type QuotaUsage struct {
	ID           string `json:"id"`
	Namespace    string `json:"namespace,omitempty"`
	Publisher    string `json:"publisher,omitempty"`
	Channel      string `json:"channel,omitempty"`
	Artifacts    int    `json:"artifacts"`
	Bytes        int64  `json:"bytes"`
	MaxArtifacts int    `json:"maxArtifacts,omitempty"`
	MaxBytes     int64  `json:"maxBytes,omitempty"`
	Exceeded     bool   `json:"exceeded"`
}

type CapacityBucket struct {
	Namespace string `json:"namespace"`
	Publisher string `json:"publisher"`
	Channel   string `json:"channel"`
	Artifacts int    `json:"artifacts"`
	Bytes     int64  `json:"bytes"`
}

type CapacityReport struct {
	CatalogRevision      uint64           `json:"catalogRevision"`
	GCRevision           uint64           `json:"gcRevision"`
	ActiveArtifacts      int              `json:"activeArtifacts"`
	ActiveBytes          int64            `json:"activeBytes"`
	QuarantinedArtifacts int              `json:"quarantinedArtifacts"`
	QuarantinedBytes     int64            `json:"quarantinedBytes"`
	SweptArtifacts       int              `json:"sweptArtifacts"`
	ReclaimedBytes       int64            `json:"reclaimedBytes"`
	StoredBytes          int64            `json:"storedBytes"`
	Buckets              []CapacityBucket `json:"buckets"`
	Quotas               []QuotaUsage     `json:"quotas"`
}

func (p QuotaPolicy) Validate() error {
	if err := validateQuotaLimit(p.QuotaLimit, true); err != nil {
		return fmt.Errorf("全局制品配额: %w", err)
	}
	ids := map[string]struct{}{}
	for _, rule := range p.Rules {
		if !quotaIDPattern.MatchString(rule.ID) {
			return errors.New("制品配额 rule ID 无效")
		}
		if _, duplicate := ids[rule.ID]; duplicate {
			return errors.New("制品配额 rule ID 重复")
		}
		ids[rule.ID] = struct{}{}
		if rule.Namespace == "" && rule.Publisher == "" && rule.Channel == "" {
			return fmt.Errorf("制品配额 rule %s 必须至少指定一个维度", rule.ID)
		}
		if (rule.Namespace != "" && !quotaNamespacePattern.MatchString(rule.Namespace)) ||
			(rule.Publisher != "" && !quotaPublisherPattern.MatchString(rule.Publisher)) ||
			(rule.Channel != "" && !quotaChannelPattern.MatchString(rule.Channel)) {
			return fmt.Errorf("制品配额 rule %s 的匹配维度无效", rule.ID)
		}
		if err := validateQuotaLimit(rule.QuotaLimit, false); err != nil {
			return fmt.Errorf("制品配额 rule %s: %w", rule.ID, err)
		}
	}
	return nil
}

func validateQuotaLimit(limit QuotaLimit, allowEmpty bool) error {
	if limit.MaxArtifacts < 0 || limit.MaxBytes < 0 {
		return errors.New("maxArtifacts/maxBytes 不能为负数")
	}
	if !allowEmpty && limit.MaxArtifacts == 0 && limit.MaxBytes == 0 {
		return errors.New("至少配置一个正数上限")
	}
	return nil
}

func (m *Manager) Capacity() CapacityReport {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.capacityLocked()
}

func (m *Manager) capacityLocked() CapacityReport {
	if m.active == nil {
		return CapacityReport{Buckets: []CapacityBucket{}, Quotas: []QuotaUsage{}}
	}
	report := CapacityReport{Buckets: []CapacityBucket{}, Quotas: []QuotaUsage{}}
	var entries []catalog.Entry
	report.CatalogRevision, entries = m.active.catalog.Entries()
	gcState := m.active.gc.List()
	report.GCRevision = gcState.Revision
	retired := make(map[string]garbagecollection.Record, len(gcState.Items))
	for _, record := range gcState.Items {
		retired[quotaRefKey(record.Ref, record.SHA256)] = record
		switch record.Status {
		case garbagecollection.StatusQuarantining, garbagecollection.StatusQuarantined, garbagecollection.StatusSweeping:
			report.QuarantinedArtifacts++
			report.QuarantinedBytes += record.Size
		case garbagecollection.StatusSwept:
			report.SweptArtifacts++
			report.ReclaimedBytes += record.Size
		}
	}
	buckets := map[string]*CapacityBucket{}
	activeEntries := make([]catalog.Entry, 0, len(entries))
	for _, entry := range entries {
		if _, ok := retired[quotaRefKey(entry.Ref, entry.SHA256)]; ok {
			continue
		}
		activeEntries = append(activeEntries, entry)
		report.ActiveArtifacts++
		report.ActiveBytes += entry.Size
		key := entry.Namespace + "\x00" + entry.Publisher + "\x00" + entry.Ref.Channel
		bucket := buckets[key]
		if bucket == nil {
			bucket = &CapacityBucket{Namespace: entry.Namespace, Publisher: entry.Publisher, Channel: entry.Ref.Channel}
			buckets[key] = bucket
		}
		bucket.Artifacts++
		bucket.Bytes += entry.Size
	}
	for _, bucket := range buckets {
		report.Buckets = append(report.Buckets, *bucket)
	}
	sort.Slice(report.Buckets, func(i, j int) bool {
		left, right := report.Buckets[i], report.Buckets[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Publisher != right.Publisher {
			return left.Publisher < right.Publisher
		}
		return left.Channel < right.Channel
	})
	report.Quotas = m.quotaUsage(activeEntries)
	report.StoredBytes = report.ActiveBytes + report.QuarantinedBytes
	return report
}

func (m *Manager) admitPublish(artifact pluginv1.Artifact) error {
	if m.active == nil {
		return errors.New("活动制品仓库不可用")
	}
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	if prior, ok := m.active.catalog.Lookup(ref); ok {
		if prior.SHA256 == artifact.SHA256 && !m.active.gc.IsRetired(prior.Ref, prior.SHA256) {
			return nil
		}
		return errors.New("不可变制品 ref 已存在或已经 retirement")
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return err
	}
	entry := catalog.Entry{Ref: ref, SHA256: artifact.SHA256, Size: artifact.Size, Publisher: manifest.Publisher, Namespace: artifactNamespace(artifact.PluginID)}
	_, existing := m.active.catalog.Entries()
	active := make([]catalog.Entry, 0, len(existing)+1)
	for _, value := range existing {
		if !m.active.gc.IsRetired(value.Ref, value.SHA256) {
			active = append(active, value)
		}
	}
	active = append(active, entry)
	for _, usage := range m.quotaUsage(active) {
		if usage.Exceeded {
			return fmt.Errorf("制品发布超过配额 %s", usage.ID)
		}
	}
	return nil
}

func (m *Manager) quotaUsage(entries []catalog.Entry) []QuotaUsage {
	values := make([]QuotaUsage, 0, len(m.quota.Rules)+1)
	if m.quota.MaxArtifacts > 0 || m.quota.MaxBytes > 0 {
		values = append(values, usageFor("global", "", "", "", m.quota.QuotaLimit, entries))
	}
	for _, rule := range m.quota.Rules {
		values = append(values, usageFor(rule.ID, rule.Namespace, rule.Publisher, rule.Channel, rule.QuotaLimit, entries))
	}
	return values
}

func usageFor(id, namespace, publisher, channel string, limit QuotaLimit, entries []catalog.Entry) QuotaUsage {
	usage := QuotaUsage{ID: id, Namespace: namespace, Publisher: publisher, Channel: channel, MaxArtifacts: limit.MaxArtifacts, MaxBytes: limit.MaxBytes}
	for _, entry := range entries {
		if namespace != "" && entry.Namespace != namespace || publisher != "" && entry.Publisher != publisher || channel != "" && entry.Ref.Channel != channel {
			continue
		}
		usage.Artifacts++
		usage.Bytes += entry.Size
	}
	usage.Exceeded = (usage.MaxArtifacts > 0 && usage.Artifacts > usage.MaxArtifacts) || (usage.MaxBytes > 0 && usage.Bytes > usage.MaxBytes)
	return usage
}

func artifactNamespace(pluginID string) string {
	if last := strings.LastIndex(pluginID, "."); last > 0 {
		return pluginID[:last]
	}
	return pluginID
}

func quotaRefKey(ref pluginv1.ArtifactRef, sha256 string) string {
	return ref.PluginID + "@" + ref.Version + "/" + ref.Channel + "\x00" + sha256
}
