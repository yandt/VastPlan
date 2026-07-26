package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const artifactLockSchemaVersion = "v1"

var (
	capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	platformPattern   = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9_]+$`)
	channelPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	publisherPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,79}$`)
)

type ResolutionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ResolutionError) Error() string {
	if e == nil {
		return "制品依赖解析失败"
	}
	return e.Message
}

type requirementConstraint struct {
	raw     string
	source  string
	value   *semver.Constraints
	channel string
}

type solveState struct {
	constraints map[string][]requirementConstraint
	selected    map[string]Entry
	features    map[string][]string
}

// Resolve atomically snapshots the current derived catalog and returns one
// deterministic exact lock. It never persists or activates the result.
func (s *Store) Resolve(request pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	if s == nil {
		return pluginv1.ArtifactLock{}, resolutionError("RESOLVER_UNAVAILABLE", "制品解析器不可用")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if err := pluginv1.ValidateArtifactResolveRequest(raw); err != nil {
		return pluginv1.ArtifactLock{}, resolutionError("REQUEST_INVALID", err.Error())
	}
	s.mu.RLock()
	revision := s.revision
	snapshot := request.SnapshotRevision
	if snapshot == 0 {
		snapshot = revision
	}
	entries := make([]Entry, 0, len(s.entries))
	for key, entry := range s.entries {
		copy := cloneEntry(entry)
		applyLifecycle(&copy, lifecycleAt(s.lifecycle[key], snapshot))
		entries = append(entries, copy)
	}
	s.mu.RUnlock()
	return resolveEntries(revision, entries, request)
}

func resolveEntries(currentRevision uint64, entries []Entry, request pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	return resolveEntriesWithBudget(currentRevision, entries, request, defaultSolveBudget)
}

func resolveEntriesWithBudget(currentRevision uint64, entries []Entry, request pluginv1.ArtifactResolveRequest, budgetLimit int) (pluginv1.ArtifactLock, error) {
	var err error
	request, err = normalizeResolveRequest(request)
	if err != nil {
		return pluginv1.ArtifactLock{}, resolutionError("REQUEST_INVALID", err.Error())
	}
	snapshot, kernelVersion, channelRank, publishers, prefixes, external, err := validateResolveRequest(currentRevision, request)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	candidates := make(map[string][]Entry)
	for _, entry := range entries {
		if entry.RepositoryRevision == 0 || entry.RepositoryRevision > snapshot || !entryAllowed(entry, request, kernelVersion, channelRank, publishers, prefixes) {
			continue
		}
		candidates[entry.Ref.PluginID] = append(candidates[entry.Ref.PluginID], entry)
	}
	for id := range candidates {
		sortCandidates(candidates[id], channelRank)
	}
	prepared, err := prepareCandidates(candidates)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}

	state := solveState{constraints: map[string][]requirementConstraint{}, selected: map[string]Entry{}, features: map[string][]string{}}
	for _, root := range request.Roots {
		constraint, parseErr := semver.NewConstraint(root.Constraint)
		if parseErr != nil {
			return pluginv1.ArtifactLock{}, resolutionError("REQUEST_INVALID", fmt.Sprintf("根依赖 %s 版本约束无效: %v", root.PluginID, parseErr))
		}
		state.constraints[root.PluginID] = append(state.constraints[root.PluginID], requirementConstraint{raw: root.Constraint, source: "root", value: constraint, channel: root.Channel})
		state.features[root.PluginID] = append([]string(nil), root.Features...)
	}
	solved, err := solve(prepared, state, external, &solveBudget{remaining: budgetLimit})
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	return buildLock(snapshot, request, solved.selected)
}

func normalizeResolveRequest(request pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactResolveRequest, error) {
	request.Roots = append([]pluginv1.ArtifactRequirement(nil), request.Roots...)
	for index := range request.Roots {
		normalized, err := pluginv1.NormalizeArtifactRequirement(request.Roots[index])
		if err != nil {
			return pluginv1.ArtifactResolveRequest{}, err
		}
		request.Roots[index] = normalized
	}
	return request, nil
}

func validateResolveRequest(current uint64, request pluginv1.ArtifactResolveRequest) (uint64, *semver.Version, map[string]int, map[string]struct{}, []string, map[string][]string, error) {
	if current == 0 {
		return 0, nil, nil, nil, nil, nil, resolutionError("CATALOG_EMPTY", "Catalog 还没有可锁定的制品")
	}
	if len(request.Roots) == 0 || len(request.Roots) > 256 {
		return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "根依赖数量必须为 1..256")
	}
	if request.Target != "backend" && request.Target != "frontend" && request.Target != "runner" && request.Target != "mobile" {
		return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "目标内核必须为 backend/frontend/runner/mobile")
	}
	kernelVersion, err := semver.NewVersion(request.KernelVersion)
	if err != nil {
		return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "目标内核版本必须是精确 SemVer")
	}
	if request.Platform != "" && !platformPattern.MatchString(request.Platform) {
		return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "目标平台必须为 os/arch")
	}
	snapshot := request.SnapshotRevision
	if snapshot == 0 {
		snapshot = current
	}
	if snapshot > current {
		return 0, nil, nil, nil, nil, nil, resolutionError("SNAPSHOT_UNAVAILABLE", fmt.Sprintf("Catalog revision %d 尚不存在，当前为 %d", snapshot, current))
	}
	channels := map[string]int{}
	for index, value := range request.AllowedChannels {
		if value == "" || len(value) > 64 || !channelPattern.MatchString(value) {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "allowedChannels 包含无效 channel")
		}
		if _, duplicate := channels[value]; duplicate {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "allowedChannels 不得重复")
		}
		channels[value] = index
	}
	if len(channels) == 0 {
		return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "allowedChannels 不能为空")
	}
	publishers := map[string]struct{}{}
	for _, value := range request.AllowedPublishers {
		if !publisherPattern.MatchString(value) {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "allowedPublishers 包含无效发布者")
		}
		if _, duplicate := publishers[value]; duplicate {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "allowedPublishers 不得重复")
		}
		publishers[value] = struct{}{}
	}
	if len(publishers) == 0 {
		return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "allowedPublishers 不能为空")
	}
	prefixes := append([]string(nil), request.AllowedPluginPrefixes...)
	for _, value := range prefixes {
		if !capabilityPattern.MatchString(value) {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "allowedPluginPrefixes 包含无效命名空间")
		}
	}
	external := map[string][]string{}
	for _, value := range request.AvailableCapabilities {
		if !capabilityPattern.MatchString(value.Capability) {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "availableCapabilities 包含无效 capability")
		}
		if value.Version != "" {
			if _, versionErr := semver.NewVersion(value.Version); versionErr != nil {
				return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "availableCapabilities 版本必须是精确 SemVer")
			}
		}
		external[value.Capability] = append(external[value.Capability], value.Version)
	}
	seenRoots := map[string]struct{}{}
	for _, root := range request.Roots {
		if !capabilityPattern.MatchString(root.PluginID) || strings.TrimSpace(root.Constraint) == "" {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "roots 包含无效插件 ID 或空约束")
		}
		if root.Channel != "" {
			if !channelPattern.MatchString(root.Channel) {
				return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "roots 包含无效 channel")
			}
			if _, allowed := channels[root.Channel]; !allowed {
				return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "roots 的精确 channel 不在 allowedChannels 中")
			}
		}
		if _, duplicate := seenRoots[root.PluginID]; duplicate {
			return 0, nil, nil, nil, nil, nil, resolutionError("REQUEST_INVALID", "roots 不得重复插件 ID")
		}
		seenRoots[root.PluginID] = struct{}{}
	}
	return snapshot, kernelVersion, channels, publishers, prefixes, external, nil
}

func entryAllowed(entry Entry, request pluginv1.ArtifactResolveRequest, kernelVersion *semver.Version, channelRank map[string]int, publishers map[string]struct{}, prefixes []string) bool {
	if entry.LifecycleStatus == LifecycleYanked || entry.LifecycleStatus == LifecycleRevoked {
		return false
	}
	if _, ok := channelRank[entry.Ref.Channel]; !ok {
		return false
	}
	if _, ok := publishers[entry.Publisher]; !ok || !allowedPrefix(entry.Ref.PluginID, prefixes) {
		return false
	}
	engineRange, ok := entry.Engines[request.Target]
	if !ok {
		return false
	}
	constraint, err := semver.NewConstraint(engineRange)
	if err != nil || !constraint.Check(kernelVersion) {
		return false
	}
	if request.Target == "backend" && len(entry.Platforms) > 0 {
		if request.Platform == "" || !contains(entry.Platforms, request.Platform) {
			return false
		}
	}
	return true
}

func allowedPrefix(pluginID string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if pluginID == prefix || strings.HasPrefix(pluginID, prefix+".") {
			return true
		}
	}
	return false
}
