package protocolbus

import (
	"fmt"
	"sort"
	"strings"
)

// CapabilityGrantPlan is the immutable host-side projection compiled from a
// verified plugin manifest. It is never accepted from plugin wire messages.
// The first phase covers kernel services; future capability grants must extend
// this plan instead of adding plugin-ID allowlists to workload policies.
type CapabilityGrantPlan struct {
	kernelServices map[string]struct{}
}

func compileCapabilityGrantPlan(kernelServices []string) (CapabilityGrantPlan, error) {
	grants := make(map[string]struct{}, len(kernelServices))
	for _, capability := range kernelServices {
		if capability == "" || strings.TrimSpace(capability) != capability {
			return CapabilityGrantPlan{}, fmt.Errorf("kernel service grant %q 无效", capability)
		}
		if _, duplicate := grants[capability]; duplicate {
			return CapabilityGrantPlan{}, fmt.Errorf("kernel service grant %q 重复", capability)
		}
		grants[capability] = struct{}{}
	}
	return CapabilityGrantPlan{kernelServices: grants}, nil
}

func (p CapabilityGrantPlan) allowsKernelService(capability string) bool {
	if capability == "" {
		return false
	}
	_, allowed := p.kernelServices[capability]
	return allowed
}

// KernelServices returns a deterministic diagnostic projection without
// exposing the plan's mutable lookup table.
func (p CapabilityGrantPlan) KernelServices() []string {
	result := make([]string, 0, len(p.kernelServices))
	for capability := range p.kernelServices {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

// kernelServiceAllowed keeps embedded execution on the same compiled-plan
// path. Process sessions compile once at handshake; embedded calls are bounded
// and compile from the already trusted launch policy on demand.
func kernelServiceAllowed(policy LaunchPolicy, capability string) bool {
	plan, err := compileCapabilityGrantPlan(policy.KernelServices)
	return err == nil && plan.allowsKernelService(capability)
}
