package edge

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

const sessionCookieName = "vastplan_session"

var ErrSessionRejected = errors.New("Portal session 无效或已过期")

// FileIdentityProvider is a minimal deployable identity boundary for the
// Portal BFF. It stores only SHA-256 token digests, rereads the file on every
// request (so revocation is immediate), and rejects files accessible to group
// or other users. Production SSO/OIDC adapters implement the same interface.
type FileIdentityProvider struct {
	path string
	now  func() time.Time
}

type sessionDocument struct {
	Sessions []sessionRecord `json:"sessions"`
}

type sessionRecord struct {
	TokenSHA256 string   `json:"tokenSHA256"`
	ID          string   `json:"id"`
	TenantID    string   `json:"tenantId"`
	Roles       []string `json:"roles"`
	ExpiresAt   string   `json:"expiresAt"`
}

func NewFileIdentityProvider(path string) (*FileIdentityProvider, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("Portal session 文件不能为空")
	}
	return &FileIdentityProvider{path: path, now: time.Now}, nil
}

func (p *FileIdentityProvider) Authenticate(r *http.Request) (portalapi.Principal, error) {
	if p == nil || r == nil {
		return portalapi.Principal{}, ErrSessionRejected
	}
	token, ok := onlyCookie(r, sessionCookieName)
	if !ok {
		return portalapi.Principal{}, ErrSessionRejected
	}
	doc, err := p.read()
	if err != nil {
		return portalapi.Principal{}, err
	}
	digest := sha256.Sum256([]byte(token))
	now := p.now().UTC()
	for _, record := range doc.Sessions {
		expected, err := hex.DecodeString(record.TokenSHA256)
		if err != nil || len(expected) != sha256.Size {
			continue
		}
		if subtle.ConstantTimeCompare(digest[:], expected) != 1 {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, record.ExpiresAt)
		if err != nil || !expiresAt.After(now) || record.ID == "" || record.TenantID == "" {
			return portalapi.Principal{}, ErrSessionRejected
		}
		return portalapi.Principal{ID: record.ID, TenantID: record.TenantID, Roles: append([]string(nil), record.Roles...)}, nil
	}
	return portalapi.Principal{}, ErrSessionRejected
}

func (p *FileIdentityProvider) read() (sessionDocument, error) {
	info, err := os.Stat(p.path)
	if err != nil {
		return sessionDocument{}, fmt.Errorf("读取 Portal session 文件: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return sessionDocument{}, errors.New("Portal session 文件必须是仅属主可读写的普通文件")
	}
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return sessionDocument{}, fmt.Errorf("读取 Portal session 文件: %w", err)
	}
	var doc sessionDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return sessionDocument{}, errors.New("Portal session 文件格式无效")
	}
	return doc, nil
}

func onlyCookie(r *http.Request, name string) (string, bool) {
	var value string
	for _, cookie := range r.Cookies() {
		if cookie.Name != name || cookie.Value == "" {
			continue
		}
		if value != "" {
			return "", false
		}
		value = cookie.Value
	}
	return value, value != ""
}
