package versionledger

import (
	"context"
	"errors"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func (s *Service) isAncestor(ctx context.Context, scope Scope, request versioningv1.IsAncestorRequest) (versioningv1.IsAncestorResult, error) {
	provider, err := s.provider(request.Ancestor.Stream.Namespace)
	if err != nil {
		return versioningv1.IsAncestorResult{}, err
	}
	distances, err := ancestorDistances(ctx, provider, scope, request.Descendant)
	if err != nil {
		return versioningv1.IsAncestorResult{}, err
	}
	distance, found := distances[request.Ancestor]
	if !found {
		return versioningv1.IsAncestorResult{Distance: -1}, nil
	}
	return versioningv1.IsAncestorResult{IsAncestor: true, Distance: distance}, nil
}

func (s *Service) findCommonAncestor(ctx context.Context, scope Scope, request versioningv1.FindCommonAncestorRequest) (versioningv1.FindCommonAncestorResult, error) {
	provider, err := s.provider(request.Left.Stream.Namespace)
	if err != nil {
		return versioningv1.FindCommonAncestorResult{}, err
	}
	left, err := ancestorDistances(ctx, provider, scope, request.Left)
	if err != nil {
		return versioningv1.FindCommonAncestorResult{}, err
	}
	right, err := ancestorDistances(ctx, provider, scope, request.Right)
	if err != nil {
		return versioningv1.FindCommonAncestorResult{}, err
	}
	var best *versioningv1.VersionRef
	bestLeft, bestRight := -1, -1
	for ref, leftDistance := range left {
		rightDistance, found := right[ref]
		if !found {
			continue
		}
		if best == nil || betterAncestor(ref, leftDistance, rightDistance, *best, bestLeft, bestRight) {
			candidate := ref
			best, bestLeft, bestRight = &candidate, leftDistance, rightDistance
		}
	}
	if best == nil {
		return versioningv1.FindCommonAncestorResult{LeftDistance: -1, RightDistance: -1}, nil
	}
	return versioningv1.FindCommonAncestorResult{Found: true, Ancestor: best, LeftDistance: bestLeft, RightDistance: bestRight}, nil
}

func ancestorDistances(ctx context.Context, provider Provider, scope Scope, start versioningv1.VersionRef) (map[versioningv1.VersionRef]int, error) {
	type queued struct {
		ref      versioningv1.VersionRef
		distance int
	}
	queue := []queued{{ref: start}}
	distances := map[versioningv1.VersionRef]int{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if previous, visited := distances[current.ref]; visited && previous <= current.distance {
			continue
		}
		if len(distances) == versioningv1.MaxAncestryNodes {
			return nil, providerError(versioningv1.ErrorLimitExceeded, false, errors.New("版本祖先遍历超过限制"))
		}
		record, err := exactProviderVersion(ctx, provider, scope, current.ref)
		if err != nil {
			return nil, err
		}
		distances[current.ref] = current.distance
		for _, parent := range record.Parents {
			queue = append(queue, queued{ref: parent, distance: current.distance + 1})
		}
	}
	return distances, nil
}

func betterAncestor(candidate versioningv1.VersionRef, left, right int, current versioningv1.VersionRef, currentLeft, currentRight int) bool {
	candidateTotal, currentTotal := left+right, currentLeft+currentRight
	if candidateTotal != currentTotal {
		return candidateTotal < currentTotal
	}
	if candidate.Sequence != current.Sequence {
		return candidate.Sequence > current.Sequence
	}
	return candidate.VersionID < current.VersionID
}
