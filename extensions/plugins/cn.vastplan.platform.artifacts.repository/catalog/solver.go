package catalog

import (
	"fmt"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

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
