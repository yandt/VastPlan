package deploymentcontroller

import (
	"sync"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

// ContractValidationCache 按 Deployment 摘要缓存由不可变 Manifest 派生的依赖图。
// Node/ActualState 事件可以重复调度同一部署，但不应重复读取并解析全部制品；
// Deployment 摘要变化后必须重新校验。
type ContractValidationCache struct {
	mu     sync.RWMutex
	digest string
	graph  map[string][]string
}

func (c *ContractValidationCache) validate(deployment deploymentv2.Deployment, graph map[string][]string, artifacts ArtifactReader) error {
	if c == nil {
		return validateDeploymentContracts(deployment, graph, artifacts)
	}
	digest := deployment.Digest()
	c.mu.RLock()
	if c.digest == digest {
		cached := cloneDependencyGraph(c.graph)
		c.mu.RUnlock()
		replaceDependencyGraph(graph, cached)
		return nil
	}
	c.mu.RUnlock()

	candidate := cloneDependencyGraph(graph)
	if err := validateDeploymentContracts(deployment, candidate, artifacts); err != nil {
		return err
	}
	c.mu.Lock()
	if c.digest != digest {
		c.digest = digest
		c.graph = cloneDependencyGraph(candidate)
	}
	cached := cloneDependencyGraph(c.graph)
	c.mu.Unlock()
	replaceDependencyGraph(graph, cached)
	return nil
}

func cloneDependencyGraph(source map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(source))
	for id, dependencies := range source {
		cloned[id] = append([]string(nil), dependencies...)
	}
	return cloned
}

func replaceDependencyGraph(target, source map[string][]string) {
	clear(target)
	for id, dependencies := range source {
		target[id] = append([]string(nil), dependencies...)
	}
}
