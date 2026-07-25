package nodeagent

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"cdsoft.com.cn/VastPlan/core/shared/go/processguard"
)

// RuntimeHostingMode decides whether compatible logical plugin units share a
// language Runtime Host. It is host policy, never a privilege requested by a
// plugin manifest.
type RuntimeHostingMode string

const (
	RuntimeHostingShared    RuntimeHostingMode = "shared"
	RuntimeHostingDedicated RuntimeHostingMode = "dedicated"
)

func validateRuntimeHostingMode(mode RuntimeHostingMode) error {
	switch mode {
	case RuntimeHostingShared, RuntimeHostingDedicated:
		return nil
	default:
		return fmt.Errorf("Runtime Host 模式无效: %q（可选: %s, %s）", mode,
			RuntimeHostingShared, RuntimeHostingDedicated)
	}
}

// RuntimeHostingPolicy uses plugin > publisher > default precedence. Security
// and ABI compatibility remain hard pool boundaries even when policy says
// shared, so configuration can reduce sharing but can never force unsafe
// co-location.
type RuntimeHostingPolicy struct {
	Default        RuntimeHostingMode
	PublisherModes map[string]RuntimeHostingMode
	PluginModes    map[string]RuntimeHostingMode
}

func ParseRuntimeHostingPolicy(defaultMode, publisherRules, pluginRules string) (RuntimeHostingPolicy, error) {
	policy := RuntimeHostingPolicy{
		Default:        RuntimeHostingMode(strings.TrimSpace(defaultMode)),
		PublisherModes: map[string]RuntimeHostingMode{},
		PluginModes:    map[string]RuntimeHostingMode{},
	}
	if policy.Default == "" {
		policy.Default = RuntimeHostingShared
	}
	if err := validateRuntimeHostingMode(policy.Default); err != nil {
		return RuntimeHostingPolicy{}, err
	}
	if err := parseRuntimeHostingRules(publisherRules, "发布者", policy.PublisherModes); err != nil {
		return RuntimeHostingPolicy{}, err
	}
	if err := parseRuntimeHostingRules(pluginRules, "插件", policy.PluginModes); err != nil {
		return RuntimeHostingPolicy{}, err
	}
	return policy, nil
}

func parseRuntimeHostingRules(raw, subject string, target map[string]RuntimeHostingMode) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	for _, item := range strings.Split(raw, ",") {
		name, value, ok := strings.Cut(item, "=")
		name = strings.TrimSpace(name)
		mode := RuntimeHostingMode(strings.TrimSpace(value))
		if !ok || name == "" || mode == "" {
			return fmt.Errorf("%s Runtime Host 规则格式无效: %q（应为 name=mode）", subject, item)
		}
		if _, duplicate := target[name]; duplicate {
			return fmt.Errorf("%s Runtime Host 规则重复: %s", subject, name)
		}
		if err := validateRuntimeHostingMode(mode); err != nil {
			return fmt.Errorf("%s %s: %w", subject, name, err)
		}
		target[name] = mode
	}
	return nil
}

func (p RuntimeHostingPolicy) modeFor(plugin InstalledPlugin) RuntimeHostingMode {
	if mode, ok := p.PluginModes[plugin.ID]; ok {
		return mode
	}
	if mode, ok := p.PublisherModes[plugin.Publisher]; ok {
		return mode
	}
	if p.Default == "" {
		return RuntimeHostingShared
	}
	return p.Default
}

// RuntimeHostKey is the complete co-location boundary. Fields are deliberately
// diagnostic rather than opaque so status and support bundles can explain why
// two plugins did not share a process.
type RuntimeHostKey struct {
	Scope         string
	Provider      string
	Isolation     IsolationLevel
	TrustDomain   string
	Compatibility string
	Generation    string
	Dedicated     string
}

func (k RuntimeHostKey) String() string {
	parts := []string{k.Scope, k.Provider, string(k.Isolation), k.TrustDomain, k.Compatibility}
	if k.Generation != "" {
		parts = append(parts, "generation="+k.Generation)
	}
	if k.Dedicated != "" {
		parts = append(parts, "dedicated="+k.Dedicated)
	}
	return strings.Join(parts, "|")
}

type runtimeHostProcessSpec struct {
	Command string
	Args    []string
	Kind    string
}

type runtimeControlRequest struct {
	RequestID   string            `json:"requestId"`
	Operation   string            `json:"operation"`
	UnitID      string            `json:"unitId,omitempty"`
	Entry       string            `json:"entry,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type runtimeControlResponse struct {
	RequestID string `json:"requestId,omitempty"`
	Event     string `json:"event,omitempty"`
	UnitID    string `json:"unitId,omitempty"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
}

type runtimeHostProcess struct {
	key    RuntimeHostKey
	spec   runtimeHostProcessSpec
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	pid    int
	logf   func(string, ...any)
	onExit func(*runtimeHostProcess)

	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan runtimeControlResponse
	units    map[string]chan error
	done     chan struct{}
	err      error
	closed   atomic.Bool
	guardian processguard.Guardian
}

type runtimeHostLogWriter struct {
	logf   func(string, ...any)
	prefix string
}

func (w runtimeHostLogWriter) Write(raw []byte) (int, error) {
	line := strings.TrimSpace(string(raw))
	if len(line) > 64<<10 {
		line = line[:64<<10] + "…[truncated]"
	}
	if line != "" && w.logf != nil {
		w.logf("%s %s", w.prefix, line)
	}
	return len(raw), nil
}
