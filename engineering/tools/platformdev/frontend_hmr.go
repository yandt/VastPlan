package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const retainedFrontendGenerations = 8

var (
	developmentModulePath = regexp.MustCompile(`^/__vastplan_dev/modules/([a-f0-9]{64})\.(?:js|css|json|wasm|bin)$`)
	sha256Value           = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type frontendHMR struct {
	root, runDir, portalURL, portalAssetsDir string
	mu                                       sync.RWMutex
	generation                               uint64
	current                                  map[string]frontendHMRModule
	objects                                  map[string]frontendHMRObject
	history                                  [][]string
	subscribers                              map[chan frontendHMREvent]struct{}
	lastError                                string
	assets                                   http.Handler
	identity                                 developmentIdentityProtocol
}

type frontendHMRModule struct {
	ID, Entry, SHA256 string
	Deferred          bool
	Graph             *frontendHMRGraph
	Digests           []string
	UIContract        *frontendHMRUIContract
	Publisher         string
	Contributions     []frontendHMRContribution
}

type frontendHMRManifest struct {
	Version int `json:"version"`
	Modules []struct {
		ID, Entry, File, SHA256 string
		Deferred                bool
		Graph                   *frontendHMRGraph
	} `json:"modules"`
}

type frontendHMRCandidate struct {
	current map[string]frontendHMRModule
	objects map[string]frontendHMRObject
}

type frontendHMREvent struct {
	Name string
	Data any
}

func (r *runtime) startFrontendHMR(ctx context.Context) error {
	portalAssetsDir := filepath.Join(r.runDir, "portal-assets")
	assets, err := newDevelopmentPortalAssets(portalAssetsDir)
	if err != nil {
		return fmt.Errorf("加载开发态 Portal 静态产物: %w", err)
	}
	hmr := &frontendHMR{
		root: r.options.root, runDir: filepath.Join(r.runDir, "frontend-hmr"), portalURL: "http://" + r.options.portalListen, portalAssetsDir: portalAssetsDir,
		current: map[string]frontendHMRModule{}, objects: map[string]frontendHMRObject{}, subscribers: map[chan frontendHMREvent]struct{}{},
		assets: assets, identity: r.identity,
	}
	if err := os.MkdirAll(hmr.runDir, 0o700); err != nil {
		return fmt.Errorf("创建前端热替换目录: %w", err)
	}
	// 开发态不能让上一次 Seed 发布的 Portal 宿主或稳定前端制品遮蔽当前
	// 工作区源码。先构建并原子切换宿主和全部插件的一代仅回环覆盖，再
	// 记录监听基线；这样启动前已经存在的修改与后续保存的修改遵循同一条
	// HMR 交付链路，Module Graph 解析器也不会落后于候选插件格式。
	if err := hmr.buildHost(ctx); err != nil {
		return fmt.Errorf("构建开发态前端源码基线: %w", err)
	}
	signatures, err := hmr.sourceSignatures()
	if err != nil {
		return err
	}
	pluginState, err := hmr.pluginWatchState()
	if err != nil {
		return err
	}
	r.hmr = hmr
	go hmr.watch(ctx, signatures, pluginState)
	log.Printf("依赖感知前端热替换已启用")
	return nil
}

func (h *frontendHMR) watch(ctx context.Context, signatures frontendSourceSignatures, pluginState frontendPluginWatchState) {
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := h.sourceSignatures()
			if err != nil {
				h.publishError(err)
				continue
			}
			if next == signatures {
				continue
			}
			hostChanged := next.host != signatures.host
			sharedChanged := next.shared != signatures.shared
			pluginsChanged := next.plugins != signatures.plugins
			signatures = next
			if hostChanged {
				err = h.buildHost(ctx)
			} else if sharedChanged || pluginsChanged {
				nextPluginState, stateErr := h.pluginWatchState()
				if stateErr != nil {
					h.publishError(stateErr)
					continue
				}
				changed, rebuildAll := changedFrontendPlugins(pluginState, nextPluginState)
				pluginState = nextPluginState
				if rebuildAll {
					err = h.buildPlugins(ctx, changed, rebuildAll)
				} else if sharedChanged {
					err = h.buildSharedHost(ctx, changed)
				} else if len(changed) > 0 {
					err = h.buildPlugins(ctx, changed, false)
				}
			}
			if err != nil {
				h.publishError(err)
			}
		}
	}
}

func (h *frontendHMR) buildPlugins(ctx context.Context, pluginIDs []string, rebuildAll bool) error {
	candidate, err := h.buildPluginCandidate(ctx, pluginIDs, rebuildAll)
	if err != nil {
		return err
	}
	h.commitCandidate(candidate, "generation", nil, rebuildAll)
	generation, _ := h.status()
	if rebuildAll {
		log.Printf("前端插件热替换候选 generation=%d 已就绪（全量）", generation)
	} else {
		log.Printf("前端插件热替换候选 generation=%d 已就绪 plugins=%s", generation, strings.Join(pluginIDs, ","))
	}
	return nil
}

func (h *frontendHMR) buildPluginCandidate(ctx context.Context, pluginIDs []string, rebuildAll bool) (frontendHMRCandidate, error) {
	h.mu.RLock()
	nextGeneration := h.generation + 1
	h.mu.RUnlock()
	directory := filepath.Join(h.runDir, fmt.Sprintf("generation-%06d", nextGeneration))
	manifest := filepath.Join(directory, "manifest.json")
	arguments := []string{"engineering/tools/build-frontend-plugins.mjs", "--development-hmr", "--include-examples", "--out-dir", directory, "--manifest", manifest}
	if !rebuildAll {
		for _, id := range pluginIDs {
			arguments = append(arguments, "--plugin", id)
		}
	}
	command := exec.CommandContext(ctx, "node", arguments...)
	command.Dir = h.root
	var output bytes.Buffer
	command.Stdout = io.MultiWriter(os.Stdout, &output)
	command.Stderr = io.MultiWriter(os.Stderr, &output)
	if err := command.Run(); err != nil {
		return frontendHMRCandidate{}, fmt.Errorf("前端插件候选构建失败: %w\n%s", err, strings.TrimSpace(output.String()))
	}
	candidate, err := h.loadCandidate(manifest)
	if err != nil {
		return frontendHMRCandidate{}, err
	}
	return candidate, nil
}

func (h *frontendHMR) buildHost(ctx context.Context) error {
	h.mu.RLock()
	nextGeneration := h.generation + 1
	h.mu.RUnlock()
	directory := filepath.Join(h.runDir, fmt.Sprintf("host-generation-%06d", nextGeneration))
	portalCandidate := filepath.Join(directory, "portal-assets")
	if err := h.runCommand(ctx, map[string]string{"PORTAL_OUT_DIR": portalCandidate, "PORTAL_DEV_HMR": "1"}, "./engineering/tools/build-frontend.sh"); err != nil {
		return fmt.Errorf("Portal 宿主候选构建失败: %w", err)
	}
	manifest := filepath.Join(directory, "modules", "manifest.json")
	if err := h.runCommand(ctx, nil, "node", "engineering/tools/build-frontend-plugins.mjs", "--development-hmr", "--include-examples", "--out-dir", filepath.Dir(manifest), "--manifest", manifest); err != nil {
		return fmt.Errorf("Portal 插件候选构建失败: %w", err)
	}
	candidate, err := h.loadCandidate(manifest)
	if err != nil {
		return err
	}
	assets, err := newDevelopmentPortalAssets(portalCandidate)
	if err != nil {
		return fmt.Errorf("验证 Portal 宿主候选: %w", err)
	}
	if err := replaceDirectory(portalCandidate, h.portalAssetsDir); err != nil {
		return fmt.Errorf("切换 Portal 宿主候选: %w", err)
	}
	h.commitCandidate(candidate, "reload", assets, true)
	log.Printf("Portal 宿主与插件候选 generation=%d 已原子切换", nextGeneration)
	return nil
}

// buildSharedHost refreshes host-provided SDK singletons and only the plugin
// modules that actually changed. Contract validation remains fail-closed; a
// breaking UI range is rejected before a mixed generation can be exposed.
func (h *frontendHMR) buildSharedHost(ctx context.Context, pluginIDs []string) error {
	h.mu.RLock()
	nextGeneration := h.generation + 1
	h.mu.RUnlock()
	directory := filepath.Join(h.runDir, fmt.Sprintf("shared-generation-%06d", nextGeneration))
	portalCandidate := filepath.Join(directory, "portal-assets")
	if err := h.runCommand(ctx, map[string]string{"PORTAL_OUT_DIR": portalCandidate, "PORTAL_DEV_HMR": "1"}, "./engineering/tools/build-frontend.sh"); err != nil {
		return fmt.Errorf("构建开发态共享 Portal 宿主候选: %w", err)
	}
	if err := validateFrontendUIContractSources(h.root); err != nil {
		return fmt.Errorf("验证共享 UI 契约兼容性: %w", err)
	}
	candidate := frontendHMRCandidate{current: map[string]frontendHMRModule{}, objects: map[string]frontendHMRObject{}}
	if len(pluginIDs) > 0 {
		var err error
		candidate, err = h.buildPluginCandidate(ctx, pluginIDs, false)
		if err != nil {
			return err
		}
	}
	assets, err := newDevelopmentPortalAssets(portalCandidate)
	if err != nil {
		return fmt.Errorf("验证开发态共享 Portal 宿主候选: %w", err)
	}
	if err := replaceDirectory(portalCandidate, h.portalAssetsDir); err != nil {
		return fmt.Errorf("切换开发态共享 Portal 宿主候选: %w", err)
	}
	h.commitCandidate(candidate, "reload", assets, false)
	generation, _ := h.status()
	log.Printf("Portal 共享 SDK 与兼容插件候选 generation=%d 已原子切换 plugins=%s", generation, strings.Join(pluginIDs, ","))
	return nil
}

func (h *frontendHMR) runCommand(ctx context.Context, extra map[string]string, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = h.root
	command.Env = mergedEnv(extra)
	var output bytes.Buffer
	command.Stdout = io.MultiWriter(os.Stdout, &output)
	command.Stderr = io.MultiWriter(os.Stderr, &output)
	if err := command.Run(); err != nil {
		return fmt.Errorf("执行 %s: %w\n%s", name, err, strings.TrimSpace(output.String()))
	}
	return nil
}

func replaceDirectory(candidate, target string) error {
	backup := fmt.Sprintf("%s.backup-%d", target, time.Now().UnixNano())
	targetExists := true
	if err := os.Rename(target, backup); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		targetExists = false
	}
	if err := os.Rename(candidate, target); err != nil {
		if targetExists {
			_ = os.Rename(backup, target)
		}
		return err
	}
	if targetExists {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func (h *frontendHMR) install(manifestPath string) error {
	return h.installCandidate(manifestPath, true)
}

func (h *frontendHMR) installCandidate(manifestPath string, replaceAll bool) error {
	candidate, err := h.loadCandidate(manifestPath)
	if err != nil {
		return err
	}
	h.commitCandidate(candidate, "generation", nil, replaceAll)
	return nil
}

func (h *frontendHMR) loadCandidate(manifestPath string) (frontendHMRCandidate, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return frontendHMRCandidate{}, fmt.Errorf("读取前端候选清单: %w", err)
	}
	var manifest frontendHMRManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Version != 1 || len(manifest.Modules) == 0 {
		return frontendHMRCandidate{}, errors.New("前端候选清单无效")
	}
	directory := filepath.Dir(manifestPath)
	current := make(map[string]frontendHMRModule, len(manifest.Modules))
	objects := map[string]frontendHMRObject{}
	for _, item := range manifest.Modules {
		relative, err := filepath.Rel(directory, item.File)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || !strings.HasPrefix(item.ID, "cn.vastplan.") || item.Entry != "frontend/dist/index.js" {
			return frontendHMRCandidate{}, fmt.Errorf("前端候选模块路径或身份无效: %s", item.ID)
		}
		content, err := os.ReadFile(item.File)
		if err != nil {
			return frontendHMRCandidate{}, err
		}
		digest := sha256.Sum256(content)
		actual := hex.EncodeToString(digest[:])
		if actual != item.SHA256 || developmentModulePath.FindStringSubmatch("/__vastplan_dev/modules/"+actual+".js") == nil {
			return frontendHMRCandidate{}, fmt.Errorf("前端候选模块摘要无效: %s", item.ID)
		}
		if _, exists := current[item.ID]; exists {
			return frontendHMRCandidate{}, fmt.Errorf("前端候选模块身份重复: %s", item.ID)
		}
		module := frontendHMRModule{ID: item.ID, Entry: item.Entry, SHA256: actual, Deferred: item.Deferred}
		if h.root != "" {
			publisher, contributions, err := readFrontendHMRContributions(h.root, item.ID)
			if err != nil {
				return frontendHMRCandidate{}, fmt.Errorf("读取前端候选贡献: %s: %w", item.ID, err)
			}
			module.Publisher, module.Contributions = publisher, contributions
		}
		contract, err := readFrontendHMRUIContract(h.root, item.ID)
		if err != nil {
			return frontendHMRCandidate{}, fmt.Errorf("读取前端候选 UI 契约: %s: %w", item.ID, err)
		}
		module.UIContract = contract
		if item.Graph == nil {
			object := frontendHMRObject{Bytes: append([]byte(nil), content...), MediaType: "text/javascript"}
			if err := addFrontendHMRObject(objects, actual, object); err != nil {
				return frontendHMRCandidate{}, fmt.Errorf("前端候选模块对象冲突: %s: %w", item.ID, err)
			}
			module.Digests = []string{actual}
		} else {
			graph, graphObjects, err := loadFrontendHMRGraph(directory, item.ID, item.Entry, actual, *item.Graph)
			if err != nil {
				return frontendHMRCandidate{}, fmt.Errorf("前端候选 Module Graph 无效: %s: %w", item.ID, err)
			}
			for digest, object := range graphObjects {
				if err := addFrontendHMRObject(objects, digest, object); err != nil {
					return frontendHMRCandidate{}, fmt.Errorf("前端候选 Module Graph 对象冲突: %s: %w", item.ID, err)
				}
				module.Digests = append(module.Digests, digest)
			}
			sort.Strings(module.Digests)
			module.Graph = &graph
		}
		current[item.ID] = module
	}
	return frontendHMRCandidate{current: current, objects: objects}, nil
}

func (h *frontendHMR) commitCandidate(candidate frontendHMRCandidate, eventName string, assets http.Handler, replaceAll bool) {
	h.mu.Lock()
	h.generation++
	if replaceAll {
		h.current = candidate.current
	} else {
		merged := make(map[string]frontendHMRModule, len(h.current)+len(candidate.current))
		for id, module := range h.current {
			merged[id] = module
		}
		for id, module := range candidate.current {
			merged[id] = module
		}
		h.current = merged
	}
	h.lastError = ""
	if assets != nil {
		h.assets = assets
	}
	for digest, object := range candidate.objects {
		h.objects[digest] = object
	}
	activeDigests := make([]string, 0)
	for _, module := range h.current {
		activeDigests = append(activeDigests, module.Digests...)
	}
	h.history = append(h.history, activeDigests)
	if len(h.history) > retainedFrontendGenerations {
		h.history = h.history[len(h.history)-retainedFrontendGenerations:]
		retained := map[string]struct{}{}
		for _, generation := range h.history {
			for _, digest := range generation {
				retained[digest] = struct{}{}
			}
		}
		for digest := range h.objects {
			if _, ok := retained[digest]; !ok {
				delete(h.objects, digest)
			}
		}
	}
	event := frontendHMREvent{Name: eventName, Data: map[string]any{"generation": h.generation}}
	h.broadcastLocked(event)
	h.mu.Unlock()
}

func (h *frontendHMR) publishError(err error) {
	message := err.Error()
	h.mu.Lock()
	h.lastError = message
	h.broadcastLocked(frontendHMREvent{Name: "build-error", Data: map[string]string{"message": message}})
	h.mu.Unlock()
	log.Printf("前端插件热替换未提交: %v", err)
}

func (h *frontendHMR) broadcastLocked(event frontendHMREvent) {
	for subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- event:
			default:
			}
		}
	}
}

func (h *frontendHMR) status() (uint64, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.generation, h.lastError
}

func (h *frontendHMR) portalAssets(w http.ResponseWriter, request *http.Request) {
	h.mu.RLock()
	assets := h.assets
	h.mu.RUnlock()
	if assets == nil {
		http.Error(w, "Portal assets unavailable", http.StatusServiceUnavailable)
		return
	}
	assets.ServeHTTP(w, request)
}

func (h *frontendHMR) events(w http.ResponseWriter, request *http.Request) {
	if !loopbackRequest(request) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	updates := make(chan frontendHMREvent, 4)
	h.mu.Lock()
	h.subscribers[updates] = struct{}{}
	if h.generation > 0 {
		updates <- frontendHMREvent{Name: "generation", Data: map[string]any{"generation": h.generation}}
	}
	if h.lastError != "" {
		updates <- frontendHMREvent{Name: "build-error", Data: map[string]string{"message": h.lastError}}
	}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subscribers, updates)
		h.mu.Unlock()
	}()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case event := <-updates:
			raw, _ := json.Marshal(event.Data)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, raw)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (h *frontendHMR) module(w http.ResponseWriter, request *http.Request) {
	if !loopbackRequest(request) || request.Method != http.MethodGet {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	match := developmentModulePath.FindStringSubmatch(request.URL.Path)
	if match == nil {
		http.NotFound(w, request)
		return
	}
	h.mu.RLock()
	object, ok := h.objects[match[1]]
	h.mu.RUnlock()
	if !ok {
		http.NotFound(w, request)
		return
	}
	contentType := object.MediaType
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		contentType += "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-VastPlan-Module-SHA256", match[1])
	_, _ = w.Write(object.Bytes)
}

func loopbackRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	return err == nil && net.ParseIP(host).IsLoopback()
}
