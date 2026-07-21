package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	developmentPortalNoncePlaceholder = "__VASTPLAN_CSP_NONCE__"
	developmentPortalRootMarker       = `<div id="vastplan-portal" aria-live="polite"></div>`
	maxDevelopmentPortalAssetFiles    = 512
	maxDevelopmentPortalAssetBytes    = 64 << 20
)

type developmentPortalAsset struct {
	content     []byte
	contentType string
	etag        string
}

// developmentPortalAssets is the development gateway's immutable in-memory
// snapshot. Production static delivery belongs exclusively to Node Portal
// Kernel; this adapter exists only so a verified host generation can switch
// atomically during local HMR.
type developmentPortalAssets struct {
	index  []byte
	assets map[string]developmentPortalAsset
}

func newDevelopmentPortalAssets(root string) (*developmentPortalAssets, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("Portal 静态产物目录不能为空")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rooted, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	defer rooted.Close()
	index, err := rooted.ReadFile("index.html")
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(index, []byte(developmentPortalNoncePlaceholder)) || !bytes.Contains(index, []byte(developmentPortalRootMarker)) {
		return nil, errors.New("Portal index.html 缺少 CSP nonce 或 SSR 宿主标记")
	}
	assets := make(map[string]developmentPortalAsset)
	totalBytes := len(index)
	err = fs.WalkDir(rooted.FS(), "assets", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return errors.New("Portal assets 只能包含普通文件")
		}
		if len(assets) >= maxDevelopmentPortalAssetFiles {
			return errors.New("Portal assets 文件数超过上限")
		}
		content, err := rooted.ReadFile(name)
		if err != nil {
			return err
		}
		totalBytes += len(content)
		if totalBytes > maxDevelopmentPortalAssetBytes {
			return errors.New("Portal assets 总大小超过上限")
		}
		digest := sha256.Sum256(content)
		contentType := mime.TypeByExtension(filepath.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		key := strings.TrimPrefix(filepath.ToSlash(name), "assets/")
		assets[key] = developmentPortalAsset{content: content, contentType: contentType, etag: `"sha256-` + hex.EncodeToString(digest[:]) + `"`}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(assets) == 0 {
		return nil, errors.New("Portal assets 目录不能为空")
	}
	return &developmentPortalAssets{index: append([]byte(nil), index...), assets: assets}, nil
}

func (a *developmentPortalAssets) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	setDevelopmentPortalSecurityHeaders(response)
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}
	if request.URL.Path == "/v1" || strings.HasPrefix(request.URL.Path, "/v1/") {
		http.NotFound(response, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/assets/") {
		a.serveAsset(response, request)
		return
	}
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		http.Error(response, "portal_nonce_unavailable", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
	body := bytes.ReplaceAll(a.index, []byte(developmentPortalNoncePlaceholder), []byte(nonce))
	response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' blob: 'nonce-"+nonce+"'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; worker-src 'none'")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		_, _ = response.Write(body)
	}
}

func (a *developmentPortalAssets) serveAsset(response http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(request.URL.Path, "/assets/")
	asset, ok := a.assets[name]
	if name == "" || !fs.ValidPath(name) || !ok {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("ETag", asset.etag)
	response.Header().Set("Cache-Control", "private, no-cache")
	response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	response.Header().Set("Content-Type", asset.contentType)
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		_, _ = response.Write(asset.content)
	}
}

func setDevelopmentPortalSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "same-origin")
	response.Header().Set("X-Frame-Options", "DENY")
	response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
}
