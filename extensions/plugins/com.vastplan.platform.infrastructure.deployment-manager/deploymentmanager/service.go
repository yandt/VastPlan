// Package deploymentmanager owns non-secret node definitions and bootstrap
// approval state. Credential material and SSH execution remain kernel-only.
package deploymentmanager

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID      = "com.vastplan.platform.infrastructure.deployment-manager"
	PluginVersion = "0.1.0"
	Capability    = platformadminapi.DeploymentCapability
	jobTTL        = 30 * time.Minute
	maxStateBytes = 16 << 20
)

var (
	errInvalid         = errors.New("部署管理请求无效")
	errNotFound        = errors.New("部署管理资源不存在")
	errVersionConflict = errors.New("节点版本冲突")
	errJobConflict     = errors.New("节点已有未完成引导作业")
	errSeparation      = errors.New("引导请求人与审批人必须不同")
	errBootstrapFailed = errors.New("可信节点引导执行失败")
)

type tenantState struct {
	Nodes map[string]platformadminapi.ManagedNode  `json:"nodes"`
	Jobs  map[string]platformadminapi.BootstrapJob `json:"jobs"`
}

type persisted struct {
	Tenants map[string]*tenantState `json:"tenants"`
}

type Service struct {
	mu    sync.Mutex
	file  string
	now   func() time.Time
	newID func() (string, error)
	data  persisted
}

func New(file string) (*Service, error) {
	if !filepath.IsAbs(file) || filepath.Clean(file) != file {
		return nil, errors.New("deployment-manager 状态文件必须是规范绝对路径")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return nil, err
	}
	if err := secureStateDirectory(filepath.Dir(file)); err != nil {
		return nil, err
	}
	s := &Service{file: file, now: func() time.Time { return time.Now().UTC() }, newID: randomID, data: persisted{Tenants: map[string]*tenantState{}}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) load() error {
	info, err := os.Lstat(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > maxStateBytes {
		return errors.New("deployment-manager 状态文件必须是仅属主可访问且大小受限的普通文件")
	}
	raw, err := os.ReadFile(s.file)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return err
	}
	if s.data.Tenants == nil {
		s.data.Tenants = map[string]*tenantState{}
	}
	if err := s.validateLoaded(); err != nil {
		return err
	}
	if s.recoverInterruptedLocked() {
		return s.saveLocked()
	}
	return nil
}

func (s *Service) validateLoaded() error {
	for tenant, state := range s.data.Tenants {
		if strings.TrimSpace(tenant) == "" || state == nil {
			return errors.New("deployment-manager 状态包含无效租户")
		}
		if state.Nodes == nil {
			state.Nodes = map[string]platformadminapi.ManagedNode{}
		}
		if state.Jobs == nil {
			state.Jobs = map[string]platformadminapi.BootstrapJob{}
		}
		for id, node := range state.Nodes {
			if id == "" || node.ID != id || node.Version < 1 || node.Plan.Node.ID != id || node.Plan.Node.Tenant != tenant || node.Plan.Validate() != nil {
				return fmt.Errorf("deployment-manager 状态包含无效节点 %q", id)
			}
		}
		for id, job := range state.Jobs {
			if id == "" || job.ID != id || job.NodeID == "" || job.NodeVersion < 1 || job.RequestedBy == "" || !validJobState(job.State) {
				return fmt.Errorf("deployment-manager 状态包含无效引导作业 %q", id)
			}
			if _, err := time.Parse(time.RFC3339Nano, job.CreatedAt); err != nil {
				return fmt.Errorf("引导作业 %q 的创建时间无效", id)
			}
			if _, err := time.Parse(time.RFC3339Nano, job.UpdatedAt); err != nil {
				return fmt.Errorf("引导作业 %q 的更新时间无效", id)
			}
			if _, err := time.Parse(time.RFC3339Nano, job.ExpiresAt); err != nil {
				return fmt.Errorf("引导作业 %q 的过期时间无效", id)
			}
			if job.State != platformadminapi.BootstrapPending && job.State != platformadminapi.BootstrapExpired && (job.ApprovedBy == "" || job.ApprovedBy == job.RequestedBy) {
				return fmt.Errorf("引导作业 %q 的审批身份无效", id)
			}
			if _, ok := state.Nodes[job.NodeID]; !ok {
				return fmt.Errorf("引导作业 %q 引用了不存在的节点", id)
			}
		}
	}
	return nil
}

func (s *Service) saveLocked() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if len(raw) > maxStateBytes {
		return errors.New("deployment-manager 状态超过上限")
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	if err := secureStateDirectory(filepath.Dir(s.file)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.file), ".deployment-manager-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.file); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(s.file))
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func secureStateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("deployment-manager 状态目录不能是符号链接或被 group/other 写入")
	}
	return nil
}

func (s *Service) recoverInterruptedLocked() bool {
	changed := false
	now := s.now().Format(time.RFC3339Nano)
	for _, state := range s.data.Tenants {
		for id, job := range state.Jobs {
			if job.State != platformadminapi.BootstrapConnecting && job.State != platformadminapi.BootstrapInstalling {
				continue
			}
			job.State = platformadminapi.BootstrapFailed
			job.ErrorCode = "platform.deployment.interrupted"
			job.UpdatedAt = now
			state.Jobs[id] = job
			changed = true
		}
	}
	return changed
}

func validJobState(state platformadminapi.BootstrapJobState) bool {
	switch state {
	case platformadminapi.BootstrapPending, platformadminapi.BootstrapApproved, platformadminapi.BootstrapConnecting,
		platformadminapi.BootstrapInstalling, platformadminapi.BootstrapSystemdActive, platformadminapi.BootstrapReady,
		platformadminapi.BootstrapFailed, platformadminapi.BootstrapExpired:
		return true
	default:
		return false
	}
}

func (s *Service) tenantLocked(tenant string) *tenantState {
	state := s.data.Tenants[tenant]
	if state == nil {
		state = &tenantState{Nodes: map[string]platformadminapi.ManagedNode{}, Jobs: map[string]platformadminapi.BootstrapJob{}}
		s.data.Tenants[tenant] = state
	}
	if state.Nodes == nil {
		state.Nodes = map[string]platformadminapi.ManagedNode{}
	}
	if state.Jobs == nil {
		state.Jobs = map[string]platformadminapi.BootstrapJob{}
	}
	return state
}

func callTenant(call *contractv1.CallContext) (string, error) {
	if call == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return "", errInvalid
	}
	return call.GetTenantId(), nil
}

func actor(call *contractv1.CallContext) (string, error) {
	if call == nil {
		return "", errInvalid
	}
	value := call.GetPrincipal().GetUserId()
	if value == "" {
		value = call.GetCaller().GetId()
	}
	if strings.TrimSpace(value) == "" {
		return "", errInvalid
	}
	return value, nil
}

func clonePlan(plan nodebootstrap.Plan) nodebootstrap.Plan {
	plan.SecretFiles = append([]nodebootstrap.CredentialSecretFile(nil), plan.SecretFiles...)
	return plan
}

func cloneNode(node platformadminapi.ManagedNode) platformadminapi.ManagedNode {
	node.Plan = clonePlan(node.Plan)
	return node
}

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
		if job.State != platformadminapi.BootstrapPending && job.State != platformadminapi.BootstrapApproved {
			continue
		}
		expires, err := time.Parse(time.RFC3339Nano, job.ExpiresAt)
		if err == nil && !now.Before(expires) {
			job.State = platformadminapi.BootstrapExpired
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

func (s *Service) Handler(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	var request struct {
		ID        string             `json:"id"`
		NodeID    string             `json:"nodeId"`
		JobID     string             `json:"jobId"`
		Plan      nodebootstrap.Plan `json:"plan"`
		IfVersion *int64             `json:"ifVersion,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return domainError("platform.deployment.invalid", errInvalid)
	}
	if err := ensureEOF(decoder); err != nil {
		return domainError("platform.deployment.invalid", errInvalid)
	}
	var out any
	var err error
	switch operation {
	case "listNodes":
		var items []platformadminapi.ManagedNode
		items, err = s.ListNodes(call)
		out = map[string]any{"items": items}
	case "putNode":
		out, err = s.PutNode(call, request.ID, platformadminapi.PutManagedNodeRequest{Plan: request.Plan, IfVersion: request.IfVersion})
	case "listBootstrapJobs":
		var items []platformadminapi.BootstrapJob
		items, err = s.ListJobs(call)
		out = map[string]any{"items": items}
	case "createBootstrap":
		out, err = s.CreateJob(call, request.NodeID)
	case "approveBootstrap":
		var job platformadminapi.BootstrapJob
		var node platformadminapi.ManagedNode
		job, node, err = s.beginApproval(call, request.JobID)
		if err == nil {
			operationName := "bootstrap"
			raw, marshalErr := json.Marshal(node.Plan)
			if marshalErr != nil {
				err = marshalErr
			} else {
				result, _, callErr := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: nodebootstrap.KernelService, Operation: &operationName}, call, raw)
				success := callErr == nil && result != nil && result.Status == contractv1.CallResult_STATUS_OK
				job, err = s.finishApproval(call, job.ID, success)
				if !success && err == nil {
					err = errBootstrapFailed
				}
			}
		}
		out = job
	default:
		err = errInvalid
	}
	if err != nil {
		return domainError(errorCode(err), err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errInvalid
	}
	return nil
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, errNotFound):
		return "platform.deployment.not_found"
	case errors.Is(err, errVersionConflict):
		return "platform.deployment.version_conflict"
	case errors.Is(err, errJobConflict):
		return "platform.deployment.job_conflict"
	case errors.Is(err, errSeparation):
		return "platform.deployment.separation_required"
	case errors.Is(err, errBootstrapFailed):
		return "platform.deployment.bootstrap_failed"
	default:
		return "platform.deployment.invalid"
	}
}

func domainError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "bootstrap-" + hex.EncodeToString(raw[:]), nil
}

func Descriptor() []byte {
	raw, _ := json.Marshal(map[string]any{"title": "节点部署管理", "subcommands": []map[string]string{{"name": "listNodes", "description": "列出节点定义"}, {"name": "putNode", "description": "保存节点定义"}, {"name": "listBootstrapJobs", "description": "列出引导作业"}, {"name": "createBootstrap", "description": "申请首次引导"}, {"name": "approveBootstrap", "description": "审批并执行首次引导"}}})
	return raw
}

func Contribution(service *Service) sdk.Contribution {
	handler := func(operation string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, call, payload, operation)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: map[string]sdk.Handler{"listNodes": handler("listNodes"), "putNode": handler("putNode"), "listBootstrapJobs": handler("listBootstrapJobs"), "createBootstrap": handler("createBootstrap"), "approveBootstrap": handler("approveBootstrap")}}
}
