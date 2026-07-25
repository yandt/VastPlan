package nodeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/processguard"
)

func startRuntimeHostProcess(key RuntimeHostKey, spec runtimeHostProcessSpec, guardian processguard.Guardian,
	logf func(string, ...any), onExit func(*runtimeHostProcess)) (*runtimeHostProcess, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return nil, errors.New("Runtime Host command 不能为空")
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	if guardian == nil {
		guardian = processguard.Default()
	}
	if err := guardian.Prepare(cmd); err != nil {
		return nil, fmt.Errorf("准备 Runtime Host 进程守护: %w", err)
	}
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
		guardian: guardian,
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
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var response runtimeControlResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			if p.logf != nil {
				p.logf("runtime-host=%s pid=%d stream=stdout %s", p.spec.Kind, p.pid,
					strings.TrimSpace(scanner.Text()))
			}
			continue
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
	if err := scanner.Err(); err != nil && p.logf != nil {
		p.logf("Runtime Host 控制输出读取失败 provider=%s pid=%d: %v", p.spec.Kind, p.pid, err)
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
		// Host 组长已退出时仍清扫同组子孙进程。
		_ = p.guardian.Kill(p.cmd)
	case <-time.After(5 * time.Second):
		_ = p.guardian.Terminate(p.cmd)
		select {
		case <-p.done:
			_ = p.guardian.Kill(p.cmd)
			return
		case <-time.After(5 * time.Second):
		}
		_ = p.guardian.Kill(p.cmd)
		<-p.done
	}
}
