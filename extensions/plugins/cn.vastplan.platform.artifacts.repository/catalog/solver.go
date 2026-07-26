package catalog

import (
	"fmt"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const defaultSolveBudget = 100_000

type solveBudget struct {
	remaining int
	evaluated int
}

type preparedDependency struct {
	raw    string
	value  *semver.Constraints
	source string
}

type preparedFeature struct {
	dependencies    map[string]preparedDependency
	runtimeRequires []pluginv1.RuntimeRequirement
}

type preparedEntry struct {
	entry        Entry
	dependencies map[string]preparedDependency
	features     map[string]preparedFeature
}

func prepareCandidates(source map[string][]Entry) (map[string][]preparedEntry, error) {
	result := make(map[string][]preparedEntry, len(source))
	ids := make([]string, 0, len(source))
	for id := range source {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, entry := range source[id] {
			dependencies, err := prepareDependencies(entry.Ref, "", entry.Dependencies)
			if err != nil {
				return nil, err
			}
			features := make(map[string]preparedFeature, len(entry.CompositionFeatures))
			featureIDs := make([]string, 0, len(entry.CompositionFeatures))
			for featureID := range entry.CompositionFeatures {
				featureIDs = append(featureIDs, featureID)
			}
			sort.Strings(featureIDs)
			for _, featureID := range featureIDs {
				feature := entry.CompositionFeatures[featureID]
				prepared, err := prepareDependencies(entry.Ref, featureID, feature.Dependencies)
				if err != nil {
					return nil, err
				}
				features[featureID] = preparedFeature{dependencies: prepared, runtimeRequires: append([]pluginv1.RuntimeRequirement(nil), feature.RuntimeRequires...)}
			}
			result[id] = append(result[id], preparedEntry{entry: entry, dependencies: dependencies, features: features})
		}
	}
	return result, nil
}

func prepareDependencies(ref pluginv1.ArtifactRef, featureID string, values map[string]string) (map[string]preparedDependency, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]preparedDependency, len(values))
	for dependencyID, raw := range values {
		constraint, err := semver.NewConstraint(raw)
		if err != nil {
			source := ""
			if featureID != "" {
				source = " Feature " + featureID
			}
			return nil, resolutionError("CATALOG_INVALID", fmt.Sprintf("制品 %s%s 的依赖 %s 约束无效", refKey(ref), source, dependencyID))
		}
		source := ref.PluginID
		if featureID != "" {
			source += "[feature:" + featureID + "]"
		}
		result[dependencyID] = preparedDependency{raw: raw, value: constraint, source: source}
	}
	return result, nil
}

func solve(candidates map[string][]preparedEntry, state solveState, external map[string][]string, budget *solveBudget) (solveState, error) {
	id := nextUnresolved(candidates, state)
	if id == "" {
		if cycle := dependencyCycle(state.selected); len(cycle) > 0 {
			return solveState{}, resolutionError("DEPENDENCY_CYCLE", "制品依赖存在环: "+strings.Join(cycle, " -> "))
		}
		if err := validateRuntimeCapabilities(state.selected, external); err != nil {
			return solveState{}, err
		}
		return state, nil
	}
	options := matchingCandidates(candidates[id], state.constraints[id])
	if len(options) == 0 {
		return solveState{}, resolutionError("VERSION_CONFLICT", fmt.Sprintf("插件 %s 无版本满足 %s", id, constraintSummary(state.constraints[id])))
	}
	var last error
	for _, candidate := range options {
		if err := budget.consume(id); err != nil {
			return solveState{}, err
		}
		active, dependencies, err := activateCandidate(candidate, state.features[id])
		if err != nil {
			last = err
			continue
		}
		next := cloneSolveState(state)
		next.selected[id] = active
		valid := true
		dependencyIDs := make([]string, 0, len(dependencies))
		for dependencyID := range dependencies {
			dependencyIDs = append(dependencyIDs, dependencyID)
		}
		sort.Strings(dependencyIDs)
		for _, dependencyID := range dependencyIDs {
			for _, dependency := range dependencies[dependencyID] {
				next.constraints[dependencyID] = append(next.constraints[dependencyID], requirementConstraint{raw: dependency.raw, source: dependency.source, value: dependency.value})
			}
			if selected, ok := next.selected[dependencyID]; ok && !constraintsMatch(selected, next.constraints[dependencyID]) {
				last = resolutionError("VERSION_CONFLICT", fmt.Sprintf("插件 %s 的已选版本 %s 不满足 %s", dependencyID, selected.Ref.Version, constraintSummary(next.constraints[dependencyID])))
				valid = false
				break
			}
			if !valid {
				break
			}
		}
		if !valid {
			continue
		}
		resolved, err := solve(candidates, next, external, budget)
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

func (budget *solveBudget) consume(pluginID string) error {
	if budget == nil || budget.remaining <= 0 {
		evaluated := 0
		if budget != nil {
			evaluated = budget.evaluated
		}
		return resolutionError("RESOLUTION_COMPLEXITY_LIMIT", fmt.Sprintf("制品依赖解析超过确定性预算: evaluated=%d plugin=%s", evaluated, pluginID))
	}
	budget.remaining--
	budget.evaluated++
	return nil
}

func activateCandidate(candidate preparedEntry, features []string) (Entry, map[string][]preparedDependency, error) {
	entry := cloneEntry(candidate.entry)
	if entry.Dependencies == nil {
		entry.Dependencies = map[string]string{}
	}
	dependencies := make(map[string][]preparedDependency, len(candidate.dependencies))
	for id, dependency := range candidate.dependencies {
		dependencies[id] = append(dependencies[id], dependency)
	}
	for _, featureID := range features {
		feature, ok := candidate.features[featureID]
		if !ok {
			return Entry{}, nil, resolutionError("FEATURE_UNAVAILABLE", fmt.Sprintf("制品 %s 未声明 Feature %s", refKey(entry.Ref), featureID))
		}
		dependencyIDs := make([]string, 0, len(feature.dependencies))
		for dependencyID := range feature.dependencies {
			dependencyIDs = append(dependencyIDs, dependencyID)
		}
		sort.Strings(dependencyIDs)
		for _, dependencyID := range dependencyIDs {
			dependency := feature.dependencies[dependencyID]
			dependencies[dependencyID] = append(dependencies[dependencyID], dependency)
			if current := entry.Dependencies[dependencyID]; current == "" {
				entry.Dependencies[dependencyID] = dependency.raw
			} else if current != dependency.raw {
				entry.Dependencies[dependencyID] = current + ", " + dependency.raw
			}
		}
		entry.RuntimeRequires = append(entry.RuntimeRequires, feature.runtimeRequires...)
	}
	return entry, dependencies, nil
}

func nextUnresolved(candidates map[string][]preparedEntry, state solveState) string {
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
	selected, minimum := ids[0], -1
	for _, id := range ids {
		count := len(matchingCandidates(candidates[id], state.constraints[id]))
		if minimum < 0 || count < minimum {
			selected, minimum = id, count
		}
	}
	return selected
}

func matchingCandidates(entries []preparedEntry, constraints []requirementConstraint) []preparedEntry {
	result := make([]preparedEntry, 0, len(entries))
	for _, entry := range entries {
		if constraintsMatch(entry.entry, constraints) {
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
		if !constraint.value.Check(version) || (constraint.channel != "" && entry.Ref.Channel != constraint.channel) {
			return false
		}
	}
	return true
}

func constraintSummary(values []requirementConstraint) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		description := value.raw
		if value.channel != "" {
			description += " @" + value.channel
		}
		parts = append(parts, fmt.Sprintf("%s (来自 %s)", description, value.source))
	}
	return strings.Join(parts, ", ")
}

func cloneSolveState(source solveState) solveState {
	out := solveState{constraints: make(map[string][]requirementConstraint, len(source.constraints)), selected: make(map[string]Entry, len(source.selected)), features: make(map[string][]string, len(source.features))}
	for id, constraints := range source.constraints {
		out.constraints[id] = append([]requirementConstraint(nil), constraints...)
	}
	for id, entry := range source.selected {
		out.selected[id] = entry
	}
	for id, features := range source.features {
		out.features[id] = append([]string(nil), features...)
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
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := selected[id]
		for _, capability := range entry.ProvidedCapabilities {
			selectedProviders[capability] = append(selectedProviders[capability], entry.Ref.Version)
		}
		for _, provided := range entry.RuntimeProvides {
			selectedProviders[provided.Capability] = append(selectedProviders[provided.Capability], entry.Ref.Version)
		}
	}
	for _, id := range ids {
		entry := selected[id]
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
