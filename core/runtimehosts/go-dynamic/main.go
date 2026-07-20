// Go Dynamic Runtime Host loads trusted first-party .so modules outside the
// Backend kernel process and exposes each module as an independent protocolbus
// session. The process is generation-scoped because Go plugins cannot unload.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"cdsoft.com.cn/VastPlan/core/runtimehosts/go-dynamic/loader"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	pluginsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var dynamicGoHostFingerprint string

type controlRequest struct {
	RequestID   string            `json:"requestId"`
	Operation   string            `json:"operation"`
	UnitID      string            `json:"unitId,omitempty"`
	Entry       string            `json:"entry,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type controlResponse struct {
	RequestID string `json:"requestId,omitempty"`
	Event     string `json:"event,omitempty"`
	UnitID    string `json:"unitId,omitempty"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
}

type controlWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func (w *controlWriter) send(response controlResponse) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.encoder.Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "Go Runtime Host 写控制响应失败: %v\n", err)
	}
}

type runtimeUnit struct {
	id       string
	plugin   *pluginsdk.Plugin
	stopping atomic.Bool
}

func (u *runtimeUnit) stop() {
	u.stopping.Store(true)
	u.plugin.Shutdown()
}

type runtimeHost struct {
	loader *loader.Loader
	writer *controlWriter
	mu     sync.Mutex
	units  map[string]*runtimeUnit
}

func newRuntimeHost() *runtimeHost {
	return &runtimeHost{
		loader: loader.New(dynamicGoHostFingerprint),
		writer: &controlWriter{encoder: json.NewEncoder(os.Stdout)},
		units:  map[string]*runtimeUnit{},
	}
}

func (h *runtimeHost) start(request controlRequest) error {
	pluginID := request.Metadata["pluginId"]
	version := request.Metadata["version"]
	fingerprint := request.Metadata["fingerprint"]
	if request.UnitID == "" || request.Entry == "" || pluginID == "" || version == "" || fingerprint == "" {
		return errors.New("dynamic-go start 缺少 unitId/entry/pluginId/version/fingerprint")
	}
	var engines map[string]string
	if err := json.Unmarshal([]byte(request.Metadata["engines"]), &engines); err != nil || len(engines) == 0 {
		return errors.New("dynamic-go start 缺少有效 engines")
	}
	h.mu.Lock()
	if h.units[request.UnitID] != nil {
		h.mu.Unlock()
		return fmt.Errorf("执行单元重复: %s", request.UnitID)
	}
	h.mu.Unlock()

	definition, err := h.loader.Load(request.Entry, pluginID, version, fingerprint)
	if err != nil {
		return err
	}
	plugin := adaptPlugin(definition, engines)
	unit := &runtimeUnit{id: request.UnitID, plugin: plugin}
	h.mu.Lock()
	if h.units[request.UnitID] != nil {
		h.mu.Unlock()
		return fmt.Errorf("执行单元重复: %s", request.UnitID)
	}
	h.units[request.UnitID] = unit
	h.mu.Unlock()

	go func() {
		err := plugin.ServeWithEnvironment(request.Environment)
		h.mu.Lock()
		delete(h.units, unit.id)
		h.mu.Unlock()
		response := controlResponse{Event: "unit-exited", UnitID: unit.id, Status: "ok"}
		if err != nil && !unit.stopping.Load() {
			response.Error = err.Error()
		}
		h.writer.send(response)
	}()
	return nil
}

func adaptPlugin(definition protocolbus.EmbeddedPlugin, engines map[string]string) *pluginsdk.Plugin {
	plugin := pluginsdk.New(definition.ID, definition.Version, engines)
	for _, contribution := range definition.Contributions {
		handlers := make(map[string]pluginsdk.Handler, len(contribution.Handlers))
		for operation, embeddedHandler := range contribution.Handlers {
			handler := embeddedHandler
			handlers[operation] = func(ctx context.Context, host pluginsdk.Host,
				callContext *contractCallContext, payload []byte) (*contractCallResult, []byte, error) {
				return handler(ctx, host, callContext, payload)
			}
		}
		plugin.Contribute(pluginsdk.Contribution{
			ExtensionPoint: contribution.ExtensionPoint, ID: contribution.ID,
			Priority: contribution.Priority, Descriptor: append([]byte(nil), contribution.Descriptor...),
			Handlers: handlers,
		})
	}
	if definition.Lifecycle != nil {
		plugin.OnLifecycle(func(ctx context.Context, lifecycle *pluginhostv1.Lifecycle) error {
			var migration *protocolbus.MigrationCommand
			switch lifecycle.Op {
			case pluginhostv1.Lifecycle_OP_MIGRATION_PREPARE,
				pluginhostv1.Lifecycle_OP_MIGRATION_COMMIT,
				pluginhostv1.Lifecycle_OP_MIGRATION_ROLLBACK:
				migration = &protocolbus.MigrationCommand{
					Operation: migrationOperation(lifecycle.Op), TransactionID: lifecycle.TransactionId,
					From: protocolbus.StateIdentity{Format: lifecycle.FromStateFormat, FormatVersion: lifecycle.FromStateVersion},
					To:   protocolbus.StateIdentity{Format: lifecycle.ToStateFormat, FormatVersion: lifecycle.ToStateVersion},
				}
			}
			return definition.Lifecycle(ctx, lifecycle.Op, migration)
		})
	}
	return plugin
}

// Local aliases keep the adapter signatures readable without changing the
// stable protocolbus DynamicGo ABI.
type contractCallContext = contractv1.CallContext
type contractCallResult = contractv1.CallResult

func migrationOperation(operation pluginhostv1.Lifecycle_Op) protocolbus.MigrationOperation {
	switch operation {
	case pluginhostv1.Lifecycle_OP_MIGRATION_PREPARE:
		return protocolbus.MigrationPrepare
	case pluginhostv1.Lifecycle_OP_MIGRATION_COMMIT:
		return protocolbus.MigrationCommit
	case pluginhostv1.Lifecycle_OP_MIGRATION_ROLLBACK:
		return protocolbus.MigrationRollback
	default:
		return ""
	}
}

func (h *runtimeHost) stop(unitID string) {
	h.mu.Lock()
	unit := h.units[unitID]
	h.mu.Unlock()
	if unit != nil {
		unit.stop()
	}
}

func (h *runtimeHost) shutdown() {
	h.mu.Lock()
	units := make([]*runtimeUnit, 0, len(h.units))
	for _, unit := range h.units {
		units = append(units, unit)
	}
	h.mu.Unlock()
	for _, unit := range units {
		unit.stop()
	}
}

func (h *runtimeHost) run() int {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var request controlRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			h.writer.send(controlResponse{Status: "error", Error: err.Error()})
			continue
		}
		response := controlResponse{RequestID: request.RequestID, UnitID: request.UnitID, Status: "ok"}
		switch request.Operation {
		case "start":
			if err := h.start(request); err != nil {
				response.Status, response.Error = "error", err.Error()
			}
		case "stop":
			h.stop(request.UnitID)
		case "shutdown":
			h.writer.send(response)
			h.shutdown()
			return 0
		default:
			response.Status, response.Error = "error", "未知控制操作: "+request.Operation
		}
		h.writer.send(response)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Go Runtime Host 读取控制请求失败: %v\n", err)
		return 1
	}
	h.shutdown()
	return 0
}

func main() {
	pool := flag.Bool("pool", false, "运行共享 Runtime Host 控制协议")
	probe := flag.Bool("probe", false, "输出 Runtime Provider 能力")
	flag.Parse()
	if *probe {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"runtime": "dynamic-go", "supported": dynamicGoHostFingerprint != "",
			"fingerprint": dynamicGoHostFingerprint,
		})
		return
	}
	if !*pool {
		fmt.Fprintln(os.Stderr, "Go Dynamic Runtime Host 必须使用 --pool")
		os.Exit(2)
	}
	os.Exit(newRuntimeHost().run())
}
