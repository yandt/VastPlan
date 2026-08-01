// Package deploymentmanager owns non-secret node definitions and bootstrap
// approval state. Credential material and SSH execution remain kernel-only.
package deploymentmanager

import (
	"errors"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
)

const (
	PluginID      = "cn.vastplan.platform.infrastructure.deployment-manager"
	PluginVersion = "0.21.10"
	Capability    = platformadminapi.DeploymentCapability
	jobTTL        = 30 * time.Minute
	maxStateBytes = 1 << 20
)

var (
	errInvalid         = errors.New("部署管理请求无效")
	errNotFound        = errors.New("部署管理资源不存在")
	errVersionConflict = errors.New("节点版本冲突")
	errJobConflict     = errors.New("节点已有未完成引导作业")
	errSeparation      = errors.New("引导请求人与审批人必须不同")
	errBootstrapFailed = errors.New("可信节点引导执行失败")
	errServiceState    = errors.New("服务组合状态不允许此操作")
	errServicePublish  = errors.New("可信服务组合发布失败")
	errStoreConflict   = errors.New("Deployment Manager Shared State 并发冲突")
)

type tenantState struct {
	Nodes                 map[string]platformadminapi.ManagedNode       `json:"nodes"`
	Jobs                  map[string]platformadminapi.BootstrapJob      `json:"jobs"`
	NextRevision          uint64                                        `json:"nextRevision"`
	NextAudit             uint64                                        `json:"nextAudit"`
	Revisions             []platformadminapi.ServiceRevision            `json:"revisions"`
	ConfigurationRequests map[string]string                             `json:"configurationRequests,omitempty"`
	ProfileActivations    map[string]profileActivationRecord            `json:"profileActivations,omitempty"`
	ServiceAudit          []platformadminapi.ServiceAuditEvent          `json:"serviceAudit"`
	TestBindings          map[string]platformadminapi.TestTargetBinding `json:"testBindings"`
	NextTestRelease       uint64                                        `json:"nextTestRelease"`
	TestReleases          []platformadminapi.TestRelease                `json:"testReleases"`
}

type persisted struct {
	Tenants map[string]*tenantState `json:"tenants"`
}

type Service struct {
	mu                  sync.Mutex
	workflowMu          sync.Mutex
	now                 func() time.Time
	newID               func() (string, error)
	data                persisted
	session             *deploymentStateSession
	testSave            func(persisted) error
	recoveredTenants    map[string]bool
	releaseTimeout      time.Duration
	releasePollInterval time.Duration
}

func New() *Service {
	return &Service{
		now: func() time.Time { return time.Now().UTC() }, newID: randomID,
		data:             persisted{Tenants: map[string]*tenantState{}},
		recoveredTenants: map[string]bool{},
		releaseTimeout:   2 * time.Minute, releasePollInterval: 500 * time.Millisecond,
	}
}
