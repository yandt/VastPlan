package protocolbus

import (
	"fmt"
	"sort"
	"strings"
)

// CapabilityGrantPlan is the immutable host-side projection compiled from the
// trusted launch policy after manifest, service-unit and publisher policy
// checks. It is never accepted from plugin wire messages. Future capability
// grants must extend this plan instead of adding plugin-ID allowlists to
// workload policies.
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

// validateKernelServiceGrants binds the operator-compiled grant list to the
// concrete services registered in this host. A missing dependency blocks the
// candidate before any plugin process or embedded unit becomes active.
func (h *Host) validateKernelServiceGrants(kernelServices []string) error {
	plan, err := compileCapabilityGrantPlan(kernelServices)
	if err != nil {
		return err
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, capability := range plan.KernelServices() {
		if _, registered := h.services[capability]; !registered {
			return fmt.Errorf("Kernel Service Grant %q 在当前宿主未注册", capability)
		}
	}
	return nil
}
