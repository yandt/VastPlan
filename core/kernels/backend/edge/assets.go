package edge

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

const portalNoncePlaceholder = "__VASTPLAN_CSP_NONCE__"

const (
	// Unicode-range self-hosted fonts intentionally produce many small immutable
	// files. Keep a bounded ceiling above the complete first-party Portal build
	// without weakening the independent total-byte limit.
	maxPortalAssetFiles = 512
	maxPortalAssetBytes = 64 << 20
)

type portalAsset struct {
	content     []byte
	contentType string
	etag        string
}

// PortalAssets serves the deployable Portal Shell without giving the browser
// access to a filesystem directory listing. Non-API routes fall back to the
// shell so client-side routing works after a refresh.
type PortalAssets struct {
	index  []byte
	assets map[string]portalAsset
}

func NewPortalAssets(root string) (*PortalAssets, error) {
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
	if bytes.Count(index, []byte(portalNoncePlaceholder)) == 0 {
		return nil, errors.New("Portal index.html 缺少 CSP nonce 占位符")
	}
	assets := make(map[string]portalAsset)
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
		if len(assets) >= maxPortalAssetFiles {
			return errors.New("Portal assets 文件数超过上限")
		}
		content, readErr := rooted.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		totalBytes += len(content)
		if totalBytes > maxPortalAssetBytes {
			return errors.New("Portal assets 总大小超过上限")
		}
		digest := sha256.Sum256(content)
		contentType := mime.TypeByExtension(filepath.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		key := strings.TrimPrefix(filepath.ToSlash(name), "assets/")
		assets[key] = portalAsset{content: content, contentType: contentType, etag: `"sha256-` + hex.EncodeToString(digest[:]) + `"`}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(assets) == 0 {
		return nil, errors.New("Portal assets 目录不能为空")
	}
	return &PortalAssets{index: index, assets: assets}, nil
}

func (a *PortalAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setPortalSecurityHeaders(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/v1" {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		a.serveAsset(w, r)
		return
	}
	a.serveIndex(w, r)
}

func (a *PortalAssets) serveIndex(w http.ResponseWriter, r *http.Request) {
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "portal_nonce_unavailable")
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
	body := bytes.ReplaceAll(a.index, []byte(portalNoncePlaceholder), []byte(nonce))
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' blob: 'nonce-"+nonce+"'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; worker-src 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(body)
	}
}

func (a *PortalAssets) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if name == "" || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	asset, ok := a.assets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("ETag", asset.etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Type", asset.contentType)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(asset.content)
	}
}

func setPortalSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
}
