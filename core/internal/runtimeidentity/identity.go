// Package runtimeidentity carries host-only evidence about one verified plugin
// execution instance. The identity is derived from LaunchPolicy and never
// accepted from plugin payloads.
package runtimeidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"cdsoft.com.cn/VastPlan/contracts/runtime/go/runtimeaudience"
)

const AudiencePrefix = runtimeaudience.Prefix

// Identity binds an execution instance to the immutable artifact and the
// kernel placement that launched it. InstanceID must change on every start.
type Identity struct {
	PluginID       string `json:"pluginId"`
	Publisher      string `json:"publisher"`
	Version        string `json:"version"`
	ArtifactSHA256 string `json:"artifactSha256"`
	NodeID         string `json:"nodeId"`
	RuntimeScope   string `json:"runtimeScope"`
	InstanceID     string `json:"instanceId"`
}

func (i Identity) Validate() error {
	fields := []struct{ name, value string }{
		{"pluginId", i.PluginID}, {"publisher", i.Publisher}, {"version", i.Version},
		{"nodeId", i.NodeID}, {"runtimeScope", i.RuntimeScope}, {"instanceId", i.InstanceID},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" || field.value != strings.TrimSpace(field.value) || len(field.value) > 256 {
			return errors.New("runtime identity " + field.name + " 无效")
		}
	}
	if len(i.ArtifactSHA256) != sha256.Size*2 {
		return errors.New("runtime identity artifactSha256 无效")
	}
	raw, err := hex.DecodeString(i.ArtifactSHA256)
	if err != nil || hex.EncodeToString(raw) != i.ArtifactSHA256 {
		return errors.New("runtime identity artifactSha256 必须是小写 SHA-256")
	}
	return nil
}

// Audience is a stable, non-secret digest used in Material Lease AAD. The
// full identity remains host-only and is not disclosed to the custodian.
func (i Identity) Audience() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return runtimeaudience.FromDigest(digest), nil
}

func ValidateAudience(audience string) error {
	return runtimeaudience.Validate(audience)
}

type contextKey struct{}

func WithIdentity(ctx context.Context, identity Identity) (context.Context, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, identity), nil
}

func FromContext(ctx context.Context) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}
	identity, ok := ctx.Value(contextKey{}).(Identity)
	return identity, ok
}
