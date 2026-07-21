package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
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
	raw    string
	source string
	value  *semver.Constraints
}

type solveState struct {
	constraints map[string][]requirementConstraint
	selected    map[string]Entry
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

	state := solveState{constraints: map[string][]requirementConstraint{}, selected: map[string]Entry{}}
	for _, root := range request.Roots {
		constraint, parseErr := semver.NewConstraint(root.Constraint)
		if parseErr != nil {
			return pluginv1.ArtifactLock{}, resolutionError("REQUEST_INVALID", fmt.Sprintf("根依赖 %s 版本约束无效: %v", root.PluginID, parseErr))
		}
		state.constraints[root.PluginID] = append(state.constraints[root.PluginID], requirementConstraint{raw: root.Constraint, source: "root", value: constraint})
	}
	solved, err := solve(candidates, state)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if cycle := dependencyCycle(solved.selected); len(cycle) > 0 {
		return pluginv1.ArtifactLock{}, resolutionError("DEPENDENCY_CYCLE", "制品依赖存在环: "+strings.Join(cycle, " -> "))
	}
	if err := validateRuntimeCapabilities(solved.selected, external); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	return buildLock(snapshot, request, solved.selected)
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

func solve(candidates map[string][]Entry, state solveState) (solveState, error) {
	id := nextUnresolved(state)
	if id == "" {
		return state, nil
	}
	options := matchingCandidates(candidates[id], state.constraints[id])
	if len(options) == 0 {
		return solveState{}, resolutionError("VERSION_CONFLICT", fmt.Sprintf("插件 %s 无版本满足 %s", id, constraintSummary(state.constraints[id])))
	}
	var last error
	for _, candidate := range options {
		next := cloneSolveState(state)
		next.selected[id] = candidate
		valid := true
		for dependencyID, raw := range candidate.Dependencies {
			constraint, err := semver.NewConstraint(raw)
			if err != nil {
				last = resolutionError("CATALOG_INVALID", fmt.Sprintf("制品 %s 的依赖 %s 约束无效", refKey(candidate.Ref), dependencyID))
				valid = false
				break
			}
			next.constraints[dependencyID] = append(next.constraints[dependencyID], requirementConstraint{raw: raw, source: id, value: constraint})
			if selected, ok := next.selected[dependencyID]; ok && !constraintsMatch(selected, next.constraints[dependencyID]) {
				last = resolutionError("VERSION_CONFLICT", fmt.Sprintf("插件 %s 的已选版本 %s 不满足 %s", dependencyID, selected.Ref.Version, constraintSummary(next.constraints[dependencyID])))
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		resolved, err := solve(candidates, next)
		if err == nil {
			return resolved, nil
		}
		last = err
	}
	if last != nil {
		return solveState{}, last
	}
	return solveState{}, resolutionError("VERSION_CONFLICT", "制品依赖无可行解")
}

func nextUnresolved(state solveState) string {
	ids := make([]string, 0, len(state.constraints))
	for id := range state.constraints {
		if _, selected := state.selected[id]; !selected {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func matchingCandidates(entries []Entry, constraints []requirementConstraint) []Entry {
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if constraintsMatch(entry, constraints) {
			result = append(result, entry)
		}
	}
	return result
}

func constraintsMatch(entry Entry, constraints []requirementConstraint) bool {
	version, err := semver.NewVersion(entry.Ref.Version)
	if err != nil {
		return false
	}
	for _, constraint := range constraints {
		if !constraint.value.Check(version) {
			return false
		}
	}
	return true
}

func constraintSummary(values []requirementConstraint) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s (来自 %s)", value.raw, value.source))
	}
	return strings.Join(parts, ", ")
}

func cloneSolveState(source solveState) solveState {
	out := solveState{constraints: make(map[string][]requirementConstraint, len(source.constraints)), selected: make(map[string]Entry, len(source.selected))}
	for id, constraints := range source.constraints {
		out.constraints[id] = append([]requirementConstraint(nil), constraints...)
	}
	for id, entry := range source.selected {
		out.selected[id] = entry
	}
	return out
}

func dependencyCycle(selected map[string]Entry) []string {
	const visiting, visited = 1, 2
	state := map[string]int{}
	stack := []string{}
	var visit func(string) []string
	visit = func(id string) []string {
		state[id] = visiting
		stack = append(stack, id)
		dependencies := make([]string, 0, len(selected[id].Dependencies))
		for dependency := range selected[id].Dependencies {
			dependencies = append(dependencies, dependency)
		}
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if _, ok := selected[dependency]; !ok {
				continue
			}
			if state[dependency] == visiting {
				start := 0
				for stack[start] != dependency {
					start++
				}
				return append(append([]string(nil), stack[start:]...), dependency)
			}
			if state[dependency] == 0 {
				if cycle := visit(dependency); len(cycle) > 0 {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = visited
		return nil
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if state[id] == 0 {
			if cycle := visit(id); len(cycle) > 0 {
				return cycle
			}
		}
	}
	return nil
}

func validateRuntimeCapabilities(selected map[string]Entry, external map[string][]string) error {
	selectedProviders := make(map[string][]string)
	for _, entry := range selected {
		for _, capability := range entry.ProvidedCapabilities {
			selectedProviders[capability] = append(selectedProviders[capability], entry.Ref.Version)
		}
		for _, provided := range entry.RuntimeProvides {
			selectedProviders[provided.Capability] = append(selectedProviders[provided.Capability], entry.Ref.Version)
		}
	}
	for _, entry := range selected {
		for _, requirement := range entry.RuntimeRequires {
			if requirement.Kind != "strong" && requirement.Kind != "data" {
				continue
			}
			if requirement.Version != "" {
				if _, err := semver.NewConstraint(requirement.Version); err != nil {
					return resolutionError("CATALOG_INVALID", fmt.Sprintf("制品 %s 的 capability %s 版本约束无效", refKey(entry.Ref), requirement.Capability))
				}
			}
			versions := append([]string(nil), selectedProviders[requirement.Capability]...)
			if requirement.Scope == "remote" {
				versions = append(versions, external[requirement.Capability]...)
			}
			if capabilitySatisfied(versions, requirement.Version) {
				continue
			}
			return resolutionError("CAPABILITY_UNSATISFIED", fmt.Sprintf("制品 %s 的阻塞依赖 capability %s %s 无提供者", refKey(entry.Ref), requirement.Capability, requirement.Version))
		}
	}
	return nil
}

func capabilitySatisfied(versions []string, rawConstraint string) bool {
	if len(versions) == 0 {
		return false
	}
	if rawConstraint == "" {
		return true
	}
	constraint, err := semver.NewConstraint(rawConstraint)
	if err != nil {
		return false
	}
	for _, raw := range versions {
		version, err := semver.NewVersion(raw)
		if err == nil && constraint.Check(version) {
			return true
		}
	}
	return false
}

func buildLock(revision uint64, request pluginv1.ArtifactResolveRequest, selected map[string]Entry) (pluginv1.ArtifactLock, error) {
	roots := append([]pluginv1.ArtifactRequirement(nil), request.Roots...)
	sort.Slice(roots, func(i, j int) bool { return roots[i].PluginID < roots[j].PluginID })
	packages := make([]pluginv1.ArtifactLockPackage, 0, len(selected))
	for _, entry := range selected {
		packages = append(packages, pluginv1.ArtifactLockPackage{
			Ref: entry.Ref, SHA256: entry.SHA256, Size: entry.Size, Publisher: entry.Publisher,
			KeyID: entry.KeyID, RepositoryRevision: entry.RepositoryRevision,
			Dependencies:    cloneStringMap(entry.Dependencies),
			LifecycleStatus: deprecatedStatus(entry), LifecycleReason: deprecatedReason(entry), Replacement: deprecatedReplacement(entry),
		})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Ref.PluginID < packages[j].Ref.PluginID })
	lock := pluginv1.ArtifactLock{
		SchemaVersion: artifactLockSchemaVersion, RepositoryRevision: revision,
		Target: request.Target, KernelVersion: request.KernelVersion, Platform: request.Platform,
		Roots: roots, Packages: packages,
	}
	digest, err := artifactLockDigest(lock)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	lock.Digest = digest
	raw, err := json.Marshal(lock)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if err := pluginv1.ValidateArtifactLock(raw); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	return lock, nil
}

func deprecatedStatus(entry Entry) string {
	if entry.LifecycleStatus == LifecycleDeprecated {
		return LifecycleDeprecated
	}
	return ""
}

func deprecatedReason(entry Entry) string {
	if entry.LifecycleStatus == LifecycleDeprecated {
		return entry.LifecycleReason
	}
	return ""
}

func deprecatedReplacement(entry Entry) *pluginv1.ArtifactRequirement {
	if entry.LifecycleStatus == LifecycleDeprecated {
		return cloneRequirement(entry.Replacement)
	}
	return nil
}

func ValidateLock(lock pluginv1.ArtifactLock) error {
	raw, err := json.Marshal(lock)
	if err != nil {
		return err
	}
	if err := pluginv1.ValidateArtifactLock(raw); err != nil {
		return err
	}
	digest, err := artifactLockDigest(lock)
	if err != nil {
		return err
	}
	if digest != lock.Digest {
		return errors.New("制品锁 digest 与规范内容不一致")
	}
	seen := map[string]struct{}{}
	locked := map[string]pluginv1.ArtifactLockPackage{}
	previous := ""
	for _, item := range lock.Packages {
		if _, duplicate := seen[item.Ref.PluginID]; duplicate {
			return fmt.Errorf("制品锁包含重复插件 ID: %s", item.Ref.PluginID)
		}
		if previous != "" && item.Ref.PluginID <= previous {
			return errors.New("制品锁 packages 必须按 pluginId 严格升序排列")
		}
		if item.RepositoryRevision > lock.RepositoryRevision {
			return fmt.Errorf("制品 %s 晚于锁定的 Catalog revision", item.Ref.PluginID)
		}
		seen[item.Ref.PluginID] = struct{}{}
		locked[item.Ref.PluginID] = item
		previous = item.Ref.PluginID
	}
	for _, item := range lock.Packages {
		for dependency, rawConstraint := range item.Dependencies {
			selected, ok := locked[dependency]
			if !ok {
				return fmt.Errorf("制品锁缺少依赖 %s -> %s", item.Ref.PluginID, dependency)
			}
			constraint, err := semver.NewConstraint(rawConstraint)
			version, versionErr := semver.NewVersion(selected.Ref.Version)
			if err != nil || versionErr != nil || !constraint.Check(version) {
				return fmt.Errorf("制品锁依赖不满足 %s -> %s %s", item.Ref.PluginID, dependency, rawConstraint)
			}
		}
	}
	rootPrevious := ""
	for _, root := range lock.Roots {
		if rootPrevious != "" && root.PluginID <= rootPrevious {
			return errors.New("制品锁 roots 必须按 pluginId 严格升序排列")
		}
		selected, ok := locked[root.PluginID]
		constraint, constraintErr := semver.NewConstraint(root.Constraint)
		version, versionErr := semver.NewVersion(selected.Ref.Version)
		if !ok || constraintErr != nil || versionErr != nil || !constraint.Check(version) {
			return fmt.Errorf("制品锁根依赖不满足: %s %s", root.PluginID, root.Constraint)
		}
		rootPrevious = root.PluginID
	}
	entries := make(map[string]Entry, len(lock.Packages))
	for _, item := range lock.Packages {
		entries[item.Ref.PluginID] = Entry{Ref: item.Ref, Dependencies: item.Dependencies}
	}
	if cycle := dependencyCycle(entries); len(cycle) > 0 {
		return errors.New("制品锁包含依赖环: " + strings.Join(cycle, " -> "))
	}
	return nil
}

func artifactLockDigest(lock pluginv1.ArtifactLock) (string, error) {
	payload := struct {
		SchemaVersion      string                         `json:"schemaVersion"`
		RepositoryRevision uint64                         `json:"repositoryRevision"`
		Target             string                         `json:"target"`
		KernelVersion      string                         `json:"kernelVersion"`
		Platform           string                         `json:"platform,omitempty"`
		Roots              []pluginv1.ArtifactRequirement `json:"roots"`
		Packages           []pluginv1.ArtifactLockPackage `json:"packages"`
	}{lock.SchemaVersion, lock.RepositoryRevision, lock.Target, lock.KernelVersion, lock.Platform, lock.Roots, lock.Packages}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func sortCandidates(entries []Entry, channelRank map[string]int) {
	sort.Slice(entries, func(i, j int) bool {
		left, leftErr := semver.NewVersion(entries[i].Ref.Version)
		right, rightErr := semver.NewVersion(entries[j].Ref.Version)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.GreaterThan(right)
		}
		if entries[i].Ref.Version != entries[j].Ref.Version {
			return entries[i].Ref.Version > entries[j].Ref.Version
		}
		if channelRank[entries[i].Ref.Channel] != channelRank[entries[j].Ref.Channel] {
			return channelRank[entries[i].Ref.Channel] < channelRank[entries[j].Ref.Channel]
		}
		return entries[i].RepositoryRevision > entries[j].RepositoryRevision
	})
}

func cloneEntry(entry Entry) Entry {
	entry.Engines = cloneStringMap(entry.Engines)
	entry.Dependencies = cloneStringMap(entry.Dependencies)
	entry.Targets = append([]string(nil), entry.Targets...)
	entry.Platforms = append([]string(nil), entry.Platforms...)
	entry.RuntimeRequires = append([]pluginv1.RuntimeRequirement(nil), entry.RuntimeRequires...)
	entry.RuntimeProvides = append([]pluginv1.RuntimeCapabilityPolicy(nil), entry.RuntimeProvides...)
	entry.ProvidedCapabilities = append([]string(nil), entry.ProvidedCapabilities...)
	return entry
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func resolutionError(code, message string) error {
	return &ResolutionError{Code: code, Message: message}
}
