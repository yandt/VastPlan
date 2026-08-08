package runtimehost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/processguard"
)

type pooledRuntimeHost struct {
	process *runtimeHostProcess
	refs    int
}

// RuntimePoolSnapshot is an operator-facing view of one physical host.
type RuntimePoolSnapshot struct {
	Key     RuntimeHostKey
	PID     int
	Units   int
	Healthy bool
}

// RuntimePoolManager owns physical language hosts. It maintains exactly one
// process per compatible shared key; dedicated mode injects a unique key. The
// first implementation intentionally has maxHostsPerPool=1 and no implicit
// overflow so overload cannot silently multiply backend processes.
type RuntimePoolManager struct {
	mu       sync.Mutex
	hosts    map[string]*pooledRuntimeHost
	retiring map[*runtimeHostProcess]struct{}
	sequence atomic.Uint64
	logf     func(string, ...any)
	guardian processguard.Guardian
	closed   bool
}

func NewRuntimePoolManager(logf func(string, ...any)) *RuntimePoolManager {
	return NewRuntimePoolManagerWithGuardian(logf, processguard.Default())
}

func NewRuntimePoolManagerWithGuardian(logf func(string, ...any), guardian processguard.Guardian) *RuntimePoolManager {
	if guardian == nil {
		guardian = processguard.Default()
	}
	return &RuntimePoolManager{
		hosts: map[string]*pooledRuntimeHost{}, retiring: map[*runtimeHostProcess]struct{}{}, logf: logf, guardian: guardian,
	}
}

type RuntimeHostLease struct {
	manager *RuntimePoolManager
	key     string
	host    *runtimeHostProcess
	unitID  string
	once    sync.Once
	failure chan error
}

func (m *RuntimePoolManager) Acquire(key RuntimeHostKey, spec runtimeHostProcessSpec) (*RuntimeHostLease, error) {
	if key.Scope == "" || key.Provider == "" || key.TrustDomain == "" || key.Compatibility == "" {
		return nil, errors.New("Runtime Pool key 的 scope/provider/trustDomain/compatibility 不能为空")
	}
	if key.Dedicated != "" {
		key.Dedicated = fmt.Sprintf("%s#%d", key.Dedicated, m.sequence.Add(1))
	}
	keyString := key.String()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("Runtime Pool Manager 已关闭")
	}
	if existing := m.hosts[keyString]; existing != nil {
		select {
		case <-existing.process.done:
			delete(m.hosts, keyString)
		default:
			if !sameRuntimeHostSpec(existing.process.spec, spec) {
				m.mu.Unlock()
				return nil, fmt.Errorf("Runtime Pool %s 的 Provider 启动规格发生漂移", keyString)
			}
			existing.refs++
			lease := &RuntimeHostLease{manager: m, key: keyString, host: existing.process,
				unitID: fmt.Sprintf("unit-%d", m.sequence.Add(1)), failure: make(chan error, 1)}
			m.mu.Unlock()
			return lease, nil
		}
	}
	m.mu.Unlock()

	process, err := startRuntimeHostProcess(key, spec, m.guardian, m.logf, m.evict)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		process.shutdown()
		return nil, errors.New("Runtime Pool Manager 已关闭")
	}
	// Another concurrent acquire may have won while this process was starting.
	// Keep only one host for a shared key and close the redundant candidate.
	select {
	case <-process.done:
		m.mu.Unlock()
		return nil, fmt.Errorf("Runtime Host %s 启动后立即退出: %v", spec.Kind, process.err)
	default:
	}
	if existing := m.hosts[keyString]; existing != nil {
		select {
		case <-existing.process.done:
			delete(m.hosts, keyString)
		default:
			if !sameRuntimeHostSpec(existing.process.spec, spec) {
				m.mu.Unlock()
				process.shutdown()
				return nil, fmt.Errorf("Runtime Pool %s 的 Provider 启动规格发生漂移", keyString)
			}
			existing.refs++
			lease := &RuntimeHostLease{manager: m, key: keyString, host: existing.process,
				unitID: fmt.Sprintf("unit-%d", m.sequence.Add(1)), failure: make(chan error, 1)}
			m.mu.Unlock()
			process.shutdown()
			return lease, nil
		}
	}
	m.hosts[keyString] = &pooledRuntimeHost{process: process, refs: 1}
	lease := &RuntimeHostLease{manager: m, key: keyString, host: process,
		unitID: fmt.Sprintf("unit-%d", m.sequence.Add(1)), failure: make(chan error, 1)}
	m.mu.Unlock()
	return lease, nil
}

func sameRuntimeHostSpec(left, right runtimeHostProcessSpec) bool {
	if left.Command != right.Command || left.Kind != right.Kind || len(left.Args) != len(right.Args) {
		return false
	}
	for index := range left.Args {
		if left.Args[index] != right.Args[index] {
			return false
		}
	}
	return true
}

func (m *RuntimePoolManager) evict(process *runtimeHostProcess) {
	m.mu.Lock()
	key := process.key.String()
	if current := m.hosts[key]; current != nil && current.process == process {
		delete(m.hosts, key)
	}
	delete(m.retiring, process)
	m.mu.Unlock()
}

func (l *RuntimeHostLease) Start(ctx context.Context, entry string, args, environment []string) error {
	return l.StartWithMetadata(ctx, entry, args, environment, nil)
}

func (l *RuntimeHostLease) StartWithMetadata(ctx context.Context, entry string, args, environment []string,
	metadata map[string]string) error {
	l.host.mu.Lock()
	l.host.units[l.unitID] = l.failure
	l.host.mu.Unlock()
	err := l.host.control(ctx, runtimeControlRequest{
		RequestID: l.unitID + "-start", Operation: "start", UnitID: l.unitID,
		Entry: entry, Args: append([]string(nil), args...), Environment: environmentMap(environment),
		Metadata: cloneRuntimeMetadata(metadata),
	})
	if err != nil {
		l.host.mu.Lock()
		delete(l.host.units, l.unitID)
		l.host.mu.Unlock()
	}
	return err
}

func cloneRuntimeMetadata(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (l *RuntimeHostLease) PID() int           { return l.host.pid }
func (l *RuntimeHostLease) UnitID() string     { return l.unitID }
func (l *RuntimeHostLease) Done() <-chan error { return l.failure }

func (l *RuntimeHostLease) Release() {
	l.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = l.host.control(ctx, runtimeControlRequest{
			RequestID: l.unitID + "-stop", Operation: "stop", UnitID: l.unitID,
		})
		cancel()
		l.host.mu.Lock()
		delete(l.host.units, l.unitID)
		l.host.mu.Unlock()
		l.manager.release(l.key, l.host)
	})
}

func (m *RuntimePoolManager) release(key string, process *runtimeHostProcess) {
	var stop *runtimeHostProcess
	m.mu.Lock()
	if current := m.hosts[key]; current != nil && current.process == process {
		current.refs--
		if current.refs <= 0 {
			delete(m.hosts, key)
			stop = current.process
			m.retiring[stop] = struct{}{}
		}
	}
	m.mu.Unlock()
	if stop != nil {
		// Session teardown owns the server stream that the managed unit is using.
		// Waiting for the physical process here would deadlock: the stream cannot
		// close until teardown returns, while the process waits for that stream.
		go stop.shutdown()
	}
}

func (m *RuntimePoolManager) Snapshot() []RuntimePoolSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]RuntimePoolSnapshot, 0, len(m.hosts))
	for _, host := range m.hosts {
		select {
		case <-host.process.done:
			result = append(result, RuntimePoolSnapshot{Key: host.process.key, PID: host.process.pid, Units: host.refs})
		default:
			result = append(result, RuntimePoolSnapshot{Key: host.process.key, PID: host.process.pid, Units: host.refs, Healthy: true})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key.String() < result[j].Key.String() })
	return result
}

func (m *RuntimePoolManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	hosts := make([]*runtimeHostProcess, 0, len(m.hosts))
	for _, host := range m.hosts {
		hosts = append(hosts, host.process)
	}
	for host := range m.retiring {
		hosts = append(hosts, host)
	}
	m.hosts = map[string]*pooledRuntimeHost{}
	m.retiring = map[*runtimeHostProcess]struct{}{}
	m.mu.Unlock()
	// 多个语言 Host 并行收敛，避免它们的优雅停机宽限期串行累加。
	var shutdownGroup sync.WaitGroup
	for _, host := range hosts {
		shutdownGroup.Add(1)
		go func(host *runtimeHostProcess) {
			defer shutdownGroup.Done()
			host.shutdown()
		}(host)
	}
	shutdownGroup.Wait()
	return nil
}

func runtimePoolKey(scope string, plugin InstalledPlugin, driver PluginExecutionDriver,
	mode RuntimeHostingMode) RuntimeHostKey {
	key := RuntimeHostKey{
		Scope: scope, Provider: driver.Name(), Isolation: driver.Isolation(),
		TrustDomain: plugin.Publisher, Compatibility: runtimeCompatibility(plugin),
	}
	if mode == RuntimeHostingDedicated {
		key.Dedicated = plugin.ID
	}
	return key
}

// managedRuntimePoolKey adds the service generation to every managed language
// host boundary. Active and candidate service generations must never share a
// physical Host: a broken candidate would otherwise be able to terminate the
// last-known-good Node/Python execution units that it is supposed to replace.
// Plugins in the same generation still share a Host when all other hard pool
// boundaries match.
func managedRuntimePoolKey(scope, generation string, plugin InstalledPlugin,
	driver PluginExecutionDriver, mode RuntimeHostingMode) (RuntimeHostKey, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "default"
	}
	generation = strings.TrimSpace(generation)
	if generation == "" {
		return RuntimeHostKey{}, fmt.Errorf("插件 %s 的托管 Runtime Host 缺少 service generation", plugin.ID)
	}
	key := runtimePoolKey(scope, plugin, driver, mode)
	key.Generation = generation
	return key, nil
}

func runtimeCompatibility(plugin InstalledPlugin) string {
	parts := []string{plugin.Execution.Driver, runtime.GOOS, runtime.GOARCH}
	keys := make([]string, 0, len(plugin.Execution.Requirements))
	for key := range plugin.Execution.Requirements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+plugin.Execution.Requirements[key])
	}
	if plugin.Execution.Node != nil {
		parts = append(parts, "node.module="+plugin.Execution.Node.ModuleFormat)
	}
	if plugin.Execution.Python != nil {
		parts = append(parts, fmt.Sprintf("python.subinterpreter-safe=%t", plugin.Execution.Python.SubinterpreterSafe))
	}
	if plugin.Execution.DynamicGo != nil {
		parts = append(parts, "dynamic-go.abi="+plugin.Execution.DynamicGo.ABI,
			"dynamic-go.fingerprint="+plugin.Execution.DynamicGo.Fingerprint)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(digest[:])
}

func environmentMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, item := range values {
		key, value, ok := strings.Cut(item, "=")
		if ok && key != "" {
			result[key] = value
		}
	}
	return result
}
