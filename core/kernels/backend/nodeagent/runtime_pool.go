package nodeagent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	Dedicated     string
}

func (k RuntimeHostKey) String() string {
	parts := []string{k.Scope, k.Provider, string(k.Isolation), k.TrustDomain, k.Compatibility}
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

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan runtimeControlResponse
	units   map[string]chan error
	done    chan struct{}
	err     error
	closed  atomic.Bool
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

func startRuntimeHostProcess(key RuntimeHostKey, spec runtimeHostProcessSpec,
	logf func(string, ...any), onExit func(*runtimeHostProcess)) (*runtimeHostProcess, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return nil, errors.New("Runtime Host command 不能为空")
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	// Runtime Hosts are trusted infrastructure, but they still must not inherit
	// arbitrary kernel secrets. Per-plugin allowlisted values are delivered only
	// in the start control message. Windows needs its system root for process
	// initialization; Unix hosts run correctly with an empty environment.
	cmd.Env = []string{}
	if runtime.GOOS == "windows" {
		for _, key := range []string{"SystemRoot", "WINDIR"} {
			if value, ok := os.LookupEnv(key); ok {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 Runtime Host 控制输入: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("创建 Runtime Host 控制输出: %w", err)
	}
	cmd.Stderr = runtimeHostLogWriter{logf: logf, prefix: "runtime-host=" + spec.Kind + " stream=stderr"}
	process := &runtimeHostProcess{
		key: key, spec: spec, cmd: cmd, stdin: stdin, logf: logf, onExit: onExit,
		pending: map[string]chan runtimeControlResponse{}, units: map[string]chan error{}, done: make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("启动 Runtime Host %s: %w", spec.Kind, err)
	}
	process.pid = cmd.Process.Pid
	go process.readResponses(stdout)
	go process.wait()
	if logf != nil {
		logf("Runtime Host 已启动 provider=%s pid=%d pool=%s", spec.Kind, process.pid, key.String())
	}
	return process, nil
}

func (p *runtimeHostProcess) readResponses(reader io.Reader) {
	decoder := json.NewDecoder(bufio.NewReader(reader))
	for {
		var response runtimeControlResponse
		if err := decoder.Decode(&response); err != nil {
			if !errors.Is(err, io.EOF) && p.logf != nil {
				p.logf("Runtime Host 控制输出无效 provider=%s pid=%d: %v", p.spec.Kind, p.pid, err)
			}
			return
		}
		if response.RequestID == "" {
			if response.Event == "unit-exited" {
				p.mu.Lock()
				failure := p.units[response.UnitID]
				p.mu.Unlock()
				if failure != nil {
					message := response.Error
					if message == "" {
						message = "Runtime Host 执行单元已退出"
					}
					select {
					case failure <- errors.New(message):
					default:
					}
				}
			}
			if response.Event == "unit-exited" && response.Error != "" && p.logf != nil {
				p.logf("Runtime Host 执行单元退出 provider=%s pid=%d unit=%s: %s",
					p.spec.Kind, p.pid, response.UnitID, response.Error)
			}
			continue
		}
		p.mu.Lock()
		waiting := p.pending[response.RequestID]
		delete(p.pending, response.RequestID)
		p.mu.Unlock()
		if waiting != nil {
			waiting <- response
			close(waiting)
		}
	}
}

func (p *runtimeHostProcess) wait() {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.err = err
	for id, waiting := range p.pending {
		waiting <- runtimeControlResponse{RequestID: id, Status: "error", Error: fmt.Sprintf("Runtime Host 已退出: %v", err)}
		close(waiting)
		delete(p.pending, id)
	}
	for id, failure := range p.units {
		select {
		case failure <- fmt.Errorf("Runtime Host 已退出: %v", err):
		default:
		}
		delete(p.units, id)
	}
	close(p.done)
	p.mu.Unlock()
	if p.onExit != nil {
		p.onExit(p)
	}
	if p.logf != nil && !p.closed.Load() {
		p.logf("Runtime Host 异常退出 provider=%s pid=%d pool=%s err=%v", p.spec.Kind, p.pid, p.key.String(), err)
	}
}

func (p *runtimeHostProcess) control(ctx context.Context, request runtimeControlRequest) error {
	if request.RequestID == "" {
		return errors.New("Runtime Host 控制请求缺少 requestId")
	}
	waiting := make(chan runtimeControlResponse, 1)
	p.mu.Lock()
	select {
	case <-p.done:
		err := p.err
		p.mu.Unlock()
		return fmt.Errorf("Runtime Host 已退出: %w", err)
	default:
	}
	p.pending[request.RequestID] = waiting
	p.mu.Unlock()

	p.writeMu.Lock()
	raw, err := json.Marshal(request)
	if err == nil {
		raw = append(raw, '\n')
		_, err = p.stdin.Write(raw)
	}
	p.writeMu.Unlock()
	if err != nil {
		p.mu.Lock()
		delete(p.pending, request.RequestID)
		p.mu.Unlock()
		return fmt.Errorf("写入 Runtime Host 控制请求: %w", err)
	}
	select {
	case response := <-waiting:
		if response.Status != "ok" {
			if response.Error == "" {
				response.Error = "Runtime Host 拒绝请求"
			}
			return errors.New(response.Error)
		}
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, request.RequestID)
		p.mu.Unlock()
		return ctx.Err()
	case <-p.done:
		p.mu.Lock()
		err := p.err
		p.mu.Unlock()
		return fmt.Errorf("Runtime Host 已退出: %w", err)
	}
}

func (p *runtimeHostProcess) shutdown() {
	if !p.closed.CompareAndSwap(false, true) {
		<-p.done
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = p.control(ctx, runtimeControlRequest{RequestID: "shutdown", Operation: "shutdown"})
	cancel()
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.done
	}
}

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
	closed   bool
}

func NewRuntimePoolManager(logf func(string, ...any)) *RuntimePoolManager {
	return &RuntimePoolManager{
		hosts: map[string]*pooledRuntimeHost{}, retiring: map[*runtimeHostProcess]struct{}{}, logf: logf,
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

	process, err := startRuntimeHostProcess(key, spec, m.logf, m.evict)
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
	l.host.mu.Lock()
	l.host.units[l.unitID] = l.failure
	l.host.mu.Unlock()
	err := l.host.control(ctx, runtimeControlRequest{
		RequestID: l.unitID + "-start", Operation: "start", UnitID: l.unitID,
		Entry: entry, Args: append([]string(nil), args...), Environment: environmentMap(environment),
	})
	if err != nil {
		l.host.mu.Lock()
		delete(l.host.units, l.unitID)
		l.host.mu.Unlock()
	}
	return err
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
	for _, host := range hosts {
		host.shutdown()
	}
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
