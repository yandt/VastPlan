package deploymentmanager

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) ListNodes(call *contractv1.CallContext) ([]platformadminapi.ManagedNode, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	items := make([]platformadminapi.ManagedNode, 0, len(state.Nodes))
	for _, node := range state.Nodes {
		items = append(items, cloneNode(node))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *Service) PutNode(call *contractv1.CallContext, id string, request platformadminapi.PutManagedNodeRequest) (platformadminapi.ManagedNode, error) {
	tenant, err := callTenant(call)
	if err != nil || strings.TrimSpace(id) == "" || request.Plan.Node.ID != id || request.Plan.Node.Tenant != tenant || request.Plan.Validate() != nil {
		return platformadminapi.ManagedNode{}, errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	for _, job := range state.Jobs {
		if job.NodeID == id && !terminal(job.State) {
			return platformadminapi.ManagedNode{}, errJobConflict
		}
	}
	old, exists := state.Nodes[id]
	if exists && (request.IfVersion == nil || *request.IfVersion != old.Version) {
		return platformadminapi.ManagedNode{}, errVersionConflict
	}
	if !exists && request.IfVersion != nil && *request.IfVersion != 0 {
		return platformadminapi.ManagedNode{}, errVersionConflict
	}
	now := s.now().Format(time.RFC3339Nano)
	version := int64(1)
	created := now
	if exists {
		version = old.Version + 1
		created = old.CreatedAt
	}
	node := platformadminapi.ManagedNode{ID: id, Plan: clonePlan(request.Plan), Version: version, CreatedAt: created, UpdatedAt: now}
	state.Nodes[id] = node
	if err := s.saveLocked(); err != nil {
		if exists {
			state.Nodes[id] = old
		} else {
			delete(state.Nodes, id)
		}
		return platformadminapi.ManagedNode{}, err
	}
	return cloneNode(node), nil
}

func (s *Service) ListJobs(call *contractv1.CallContext) ([]platformadminapi.BootstrapJob, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	changed := s.expireLocked(state)
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	items := make([]platformadminapi.BootstrapJob, 0, len(state.Jobs))
	for _, job := range state.Jobs {
		items = append(items, job)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

func (s *Service) refreshReadiness(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	tenant, err := callTenant(call)
	if err != nil || host == nil {
		return errInvalid
	}
	type candidate struct {
		job  platformadminapi.BootstrapJob
		node platformadminapi.ManagedNode
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	changed := s.expireLocked(state)
	candidates := make([]candidate, 0)
	for _, job := range state.Jobs {
		if job.State != platformadminapi.BootstrapSystemdActive {
			continue
		}
		node, ok := state.Nodes[job.NodeID]
		if ok && node.Version == job.NodeVersion {
			candidates = append(candidates, candidate{job: job, node: cloneNode(node)})
		}
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()

	for _, item := range candidates {
		expectation := nodebootstrap.ReadinessExpectation{
			TenantID: tenant, NodeID: item.node.ID, Deployment: item.node.Plan.Node.Deployment,
			TransportPublicKey: item.node.Plan.Node.TransportPublicKey,
		}
		raw, marshalErr := json.Marshal(expectation)
		if marshalErr != nil {
			continue
		}
		operation := "observe"
		result, response, callErr := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: nodebootstrap.KernelReadinessService, Operation: &operation}, call, raw)
		if callErr != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
			continue
		}
		var observation nodebootstrap.ReadinessObservation
		if err := json.Unmarshal(response, &observation); err != nil {
			continue
		}
		if observation.Status != nodebootstrap.ReadinessReady && observation.Status != nodebootstrap.ReadinessRejected {
			continue
		}
		s.mu.Lock()
		state = s.tenantLocked(tenant)
		current, ok := state.Jobs[item.job.ID]
		if !ok || current.State != platformadminapi.BootstrapSystemdActive || current.NodeVersion != item.job.NodeVersion {
			s.mu.Unlock()
			continue
		}
		old := current
		current.UpdatedAt = s.now().Format(time.RFC3339Nano)
		if observation.Status == nodebootstrap.ReadinessReady {
			current.State = platformadminapi.BootstrapReady
			current.ErrorCode = ""
		} else {
			current.State = platformadminapi.BootstrapFailed
			current.ErrorCode = "platform.deployment.readiness_rejected"
		}
		state.Jobs[current.ID] = current
		if err := s.saveLocked(); err != nil {
			state.Jobs[current.ID] = old
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *Service) job(call *contractv1.CallContext, id string) (platformadminapi.BootstrapJob, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.BootstrapJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.tenantLocked(tenant).Jobs[id]
	if !ok {
		return platformadminapi.BootstrapJob{}, errNotFound
	}
	return job, nil
}

func (s *Service) CreateJob(call *contractv1.CallContext, nodeID string) (platformadminapi.BootstrapJob, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.BootstrapJob{}, err
	}
	requester, err := actor(call)
	if err != nil {
		return platformadminapi.BootstrapJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	if s.expireLocked(state) {
		if err := s.saveLocked(); err != nil {
			return platformadminapi.BootstrapJob{}, err
		}
	}
	node, exists := state.Nodes[nodeID]
	if !exists {
		return platformadminapi.BootstrapJob{}, errNotFound
	}
	for _, job := range state.Jobs {
		if job.NodeID == nodeID && !terminal(job.State) {
			return platformadminapi.BootstrapJob{}, errJobConflict
		}
	}
	id, err := s.newID()
	if err != nil {
		return platformadminapi.BootstrapJob{}, err
	}
	now := s.now()
	job := platformadminapi.BootstrapJob{ID: id, NodeID: nodeID, NodeVersion: node.Version, State: platformadminapi.BootstrapPending, RequestedBy: requester, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(jobTTL).Format(time.RFC3339Nano)}
	state.Jobs[id] = job
	if err := s.saveLocked(); err != nil {
		delete(state.Jobs, id)
		return platformadminapi.BootstrapJob{}, err
	}
	return job, nil
}

func (s *Service) beginApproval(call *contractv1.CallContext, jobID string) (platformadminapi.BootstrapJob, platformadminapi.ManagedNode, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, err
	}
	approver, err := actor(call)
	if err != nil {
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	if s.expireLocked(state) {
		if err := s.saveLocked(); err != nil {
			return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, err
		}
	}
	job, exists := state.Jobs[jobID]
	if !exists {
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, errNotFound
	}
	if job.State != platformadminapi.BootstrapPending && job.State != platformadminapi.BootstrapApproved {
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, errJobConflict
	}
	if job.RequestedBy == approver {
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, errSeparation
	}
	if job.State == platformadminapi.BootstrapApproved && job.ApprovedBy != approver {
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, errJobConflict
	}
	node, exists := state.Nodes[job.NodeID]
	if !exists || node.Version != job.NodeVersion {
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, errVersionConflict
	}
	old := job
	now := s.now().Format(time.RFC3339Nano)
	if job.State == platformadminapi.BootstrapPending {
		job.State = platformadminapi.BootstrapApproved
		job.ApprovedBy = approver
		job.UpdatedAt = now
		state.Jobs[jobID] = job
		if err := s.saveLocked(); err != nil {
			state.Jobs[jobID] = old
			return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, err
		}
	}
	job.State = platformadminapi.BootstrapConnecting
	job.UpdatedAt = now
	state.Jobs[jobID] = job
	if err := s.saveLocked(); err != nil {
		job.State = platformadminapi.BootstrapApproved
		state.Jobs[jobID] = job
		return platformadminapi.BootstrapJob{}, platformadminapi.ManagedNode{}, err
	}
	return job, cloneNode(node), nil
}

func (s *Service) finishApproval(call *contractv1.CallContext, jobID string, success bool) (platformadminapi.BootstrapJob, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.BootstrapJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	job, exists := state.Jobs[jobID]
	if !exists || job.State != platformadminapi.BootstrapConnecting {
		return platformadminapi.BootstrapJob{}, errJobConflict
	}
	old := job
	job.UpdatedAt = s.now().Format(time.RFC3339Nano)
	if success {
		job.State = platformadminapi.BootstrapSystemdActive
		job.ErrorCode = ""
	} else {
		job.State = platformadminapi.BootstrapFailed
		job.ErrorCode = "platform.deployment.bootstrap_failed"
	}
	state.Jobs[jobID] = job
	if err := s.saveLocked(); err != nil {
		state.Jobs[jobID] = old
		return platformadminapi.BootstrapJob{}, err
	}
	return job, nil
}

func (s *Service) expireLocked(state *tenantState) bool {
	now := s.now()
	changed := false
	for id, job := range state.Jobs {
		if job.State != platformadminapi.BootstrapPending && job.State != platformadminapi.BootstrapApproved && job.State != platformadminapi.BootstrapSystemdActive {
			continue
		}
		expires, err := time.Parse(time.RFC3339Nano, job.ExpiresAt)
		if err == nil && !now.Before(expires) {
			if job.State == platformadminapi.BootstrapSystemdActive {
				job.State = platformadminapi.BootstrapFailed
				job.ErrorCode = "platform.deployment.readiness_timeout"
			} else {
				job.State = platformadminapi.BootstrapExpired
			}
			job.UpdatedAt = now.Format(time.RFC3339Nano)
			state.Jobs[id] = job
			changed = true
		}
	}
	return changed
}

func terminal(state platformadminapi.BootstrapJobState) bool {
	switch state {
	case platformadminapi.BootstrapSystemdActive, platformadminapi.BootstrapReady, platformadminapi.BootstrapFailed, platformadminapi.BootstrapExpired:
		return true
	default:
		return false
	}
}
