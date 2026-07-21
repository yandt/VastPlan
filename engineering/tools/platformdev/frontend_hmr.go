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
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const retainedFrontendGenerations = 8

var developmentModulePath = regexp.MustCompile(`^/__vastplan_dev/modules/([a-f0-9]{64})\.js$`)

type frontendHMR struct {
	root, runDir, portalListen, portalAssetsDir string
	mu                                          sync.RWMutex
	generation                                  uint64
	current                                     map[string]frontendHMRModule
	objects                                     map[string][]byte
	history                                     [][]string
	subscribers                                 map[chan frontendHMREvent]struct{}
	lastError                                   string
	assets                                      http.Handler
}

type frontendSourceSignatures struct{ plugins, host string }

type frontendHMRModule struct {
	ID, Entry, SHA256 string
	Bytes             []byte
	Deferred          bool
}

type frontendHMRManifest struct {
	Version int `json:"version"`
	Modules []struct {
		ID, Entry, File, SHA256 string
		Deferred                bool
	} `json:"modules"`
}

type frontendHMRCandidate struct {
	current map[string]frontendHMRModule
	digests []string
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
		root: r.options.root, runDir: filepath.Join(r.runDir, "frontend-hmr"), portalListen: r.options.portalListen, portalAssetsDir: portalAssetsDir,
		current: map[string]frontendHMRModule{}, objects: map[string][]byte{}, subscribers: map[chan frontendHMREvent]struct{}{},
		assets: assets,
	}
	if err := os.MkdirAll(hmr.runDir, 0o700); err != nil {
		return fmt.Errorf("创建前端热替换目录: %w", err)
	}
	signatures, err := hmr.sourceSignatures()
	if err != nil {
		return err
	}
	r.hmr = hmr
	go hmr.watch(ctx, signatures)
	log.Printf("依赖感知前端热替换已启用")
	return nil
}

func (h *frontendHMR) watch(ctx context.Context, signatures frontendSourceSignatures) {
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
			pluginsChanged := next.plugins != signatures.plugins
			signatures = next
			if hostChanged {
				err = h.buildHost(ctx)
			} else if pluginsChanged {
				err = h.buildPlugins(ctx)
			}
			if err != nil {
				h.publishError(err)
			}
		}
	}
}

func (h *frontendHMR) sourceSignatures() (frontendSourceSignatures, error) {
	plugins, err := sourceSignature(h.root, []string{
		"extensions/plugins",
		"extensions/sdk/ts/platform-admin/src",
		"extensions/sdk/ts/platform-admin/package.json",
	})
	if err != nil {
		return frontendSourceSignatures{}, fmt.Errorf("扫描前端插件源码: %w", err)
	}
	host, err := sourceSignature(h.root, []string{
		"core/kernels/frontend/src",
		"core/kernels/frontend/static",
		"core/kernels/frontend/package.json",
		"extensions/sdk/ts/ui-primitives/src",
		"extensions/sdk/ts/ui-primitives/package.json",
		"extensions/sdk/ts/ui-contract/src",
		"extensions/sdk/ts/ui-contract/package.json",
		"extensions/sdk/ts/workbench-sdk/src",
		"extensions/sdk/ts/workbench-sdk/package.json",
		"engineering/tools/build-frontend.sh",
		"engineering/tools/build-frontend-plugins.mjs",
		"package.json",
		"pnpm-lock.yaml",
		"pnpm-workspace.yaml",
		"tsconfig.base.json",
	})
	if err != nil {
		return frontendSourceSignatures{}, fmt.Errorf("扫描 Portal 宿主源码: %w", err)
	}
	return frontendSourceSignatures{plugins: plugins, host: host}, nil
}

func sourceSignature(root string, relativePaths []string) (string, error) {
	hash := sha256.New()
	for _, relativeRoot := range relativePaths {
		absoluteRoot := filepath.Join(root, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			switch filepath.Ext(path) {
			case ".ts", ".tsx", ".css", ".json", ".mjs", ".sh", ".html":
			default:
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, filepath.ToSlash(relative))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write(content)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (h *frontendHMR) buildPlugins(ctx context.Context) error {
	h.mu.RLock()
	nextGeneration := h.generation + 1
	h.mu.RUnlock()
	directory := filepath.Join(h.runDir, fmt.Sprintf("generation-%06d", nextGeneration))
	manifest := filepath.Join(directory, "manifest.json")
	command := exec.CommandContext(ctx, "node", "engineering/tools/build-frontend-plugins.mjs", "--out-dir", directory, "--manifest", manifest)
	command.Dir = h.root
	var output bytes.Buffer
	command.Stdout = io.MultiWriter(os.Stdout, &output)
	command.Stderr = io.MultiWriter(os.Stderr, &output)
	if err := command.Run(); err != nil {
		return fmt.Errorf("前端插件候选构建失败: %w\n%s", err, strings.TrimSpace(output.String()))
	}
	if err := h.install(manifest); err != nil {
		return err
	}
	log.Printf("前端插件热替换候选 generation=%d 已就绪", nextGeneration)
	return nil
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
	if err := h.runCommand(ctx, nil, "node", "engineering/tools/build-frontend-plugins.mjs", "--out-dir", filepath.Dir(manifest), "--manifest", manifest); err != nil {
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
	h.commitCandidate(candidate, "reload", assets)
	log.Printf("Portal 宿主与插件候选 generation=%d 已原子切换", nextGeneration)
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
	candidate, err := h.loadCandidate(manifestPath)
	if err != nil {
		return err
	}
	h.commitCandidate(candidate, "generation", nil)
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
	digests := make([]string, 0, len(manifest.Modules))
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
		copied := append([]byte(nil), content...)
		current[item.ID] = frontendHMRModule{ID: item.ID, Entry: item.Entry, SHA256: actual, Bytes: copied, Deferred: item.Deferred}
		digests = append(digests, actual)
	}
	return frontendHMRCandidate{current: current, digests: digests}, nil
}

func (h *frontendHMR) commitCandidate(candidate frontendHMRCandidate, eventName string, assets http.Handler) {
	h.mu.Lock()
	h.generation++
	h.current = candidate.current
	h.lastError = ""
	if assets != nil {
		h.assets = assets
	}
	for _, module := range candidate.current {
		h.objects[module.SHA256] = module.Bytes
	}
	h.history = append(h.history, candidate.digests)
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
	content, ok := h.objects[match[1]]
	h.mu.RUnlock()
	if !ok {
		http.NotFound(w, request)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-VastPlan-Module-SHA256", match[1])
	_, _ = w.Write(content)
}

func (h *frontendHMR) runtime(w http.ResponseWriter, request *http.Request) {
	if !loopbackRequest(request) || request.Method != http.MethodGet {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	target := "https://" + h.portalListen + "/v1/portal-runtime"
	if request.URL.RawQuery != "" {
		target += "?" + request.URL.RawQuery
	}
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	upstream.AddCookie(&http.Cookie{Name: "vastplan_session", Value: devAdminToken})
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: insecureLocalTLS()}}
	response, err := client.Do(upstream)
	if err != nil {
		http.Error(w, "Portal Runtime upstream unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		http.Error(w, "Portal Runtime upstream rejected request", response.StatusCode)
		return
	}
	var document struct {
		Portal  json.RawMessage  `json:"portal"`
		Modules []map[string]any `json:"modules"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		http.Error(w, "Portal Runtime upstream invalid", http.StatusBadGateway)
		return
	}
	h.mu.RLock()
	for _, descriptor := range document.Modules {
		id, _ := descriptor["id"].(string)
		if module, ok := h.current[id]; ok {
			descriptor["entry"] = module.Entry
			descriptor["url"] = "/__vastplan_dev/modules/" + module.SHA256 + ".js"
			descriptor["sha256"] = module.SHA256
			descriptor["deferred"] = module.Deferred
		}
	}
	h.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(document)
}

func loopbackRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	return err == nil && net.ParseIP(host).IsLoopback()
}
