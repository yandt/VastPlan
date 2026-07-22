// Package pluginconfiguration defines the trusted, browser-safe catalog used
// by the generic plugin configuration control plane. Catalogs are derived only
// from artifacts already verified by the Backend composition resolver.
package pluginconfiguration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

const SchemaVersion = "v1"

const (
	PluginSettingsID      = "cn.vastplan.platform.configuration.plugin-settings"
	KernelCatalogsService = "kernel.configuration.catalogs"
)

// Reader and Publisher keep the configuration catalog control-plane contract
// independent from its NATS KV adapter. Only trusted kernel composition roots
// provide these interfaces to plugin hosts and deployment publishers.
type Reader interface {
	List(context.Context, string) ([]Catalog, error)
}

type Publisher interface {
	Publish(context.Context, string, Catalog) error
}

type ApplyPath string

const (
	ApplyApplicationDeployment ApplyPath = "application-deployment"
	ApplyPlatformProfile       ApplyPath = "platform-profile"
	ApplyHotService            ApplyPath = "hot-service"
	ApplyHotScoped             ApplyPath = "hot-scoped"
)

// ArtifactIdentity is the immutable package identity that supplied a
// configuration contract. Object paths and repository endpoints are omitted.
type ArtifactIdentity struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
	SHA256  string `json:"sha256"`
}

// Definition is safe for the management plane. Values are non-sensitive by
// manifest contract; managed credential handles and material are never present.
type Definition struct {
	ID                 string                            `json:"id"`
	Deployment         string                            `json:"deployment"`
	UnitID             string                            `json:"unitId"`
	PluginID           string                            `json:"pluginId"`
	PluginName         string                            `json:"pluginName"`
	Origin             string                            `json:"origin"`
	Artifact           ArtifactIdentity                  `json:"artifact"`
	Scope              string                            `json:"scope"`
	ApplyMode          string                            `json:"applyMode"`
	ApplyPath          ApplyPath                         `json:"applyPath"`
	Schema             json.RawMessage                   `json:"schema"`
	SchemaDigest       string                            `json:"schemaDigest"`
	ManagedCredentials []pluginv1.ManagedCredentialField `json:"managedCredentials,omitempty"`
	Values             json.RawMessage                   `json:"values"`
	DeploymentRevision uint64                            `json:"deploymentRevision"`
	DeploymentDigest   string                            `json:"deploymentDigest"`
}

// Catalog is a complete replacement snapshot for one resolved Deployment.
type Catalog struct {
	SchemaVersion      string       `json:"schemaVersion"`
	Deployment         string       `json:"deployment"`
	DeploymentRevision uint64       `json:"deploymentRevision"`
	DeploymentDigest   string       `json:"deploymentDigest"`
	Items              []Definition `json:"items"`
	Digest             string       `json:"digest"`
}

// Build derives a catalog from the exact artifacts observed while resolving a
// Deployment. Callers must not supply artifacts from a separate mutable lookup.
func Build(deployment deploymentv2.Deployment, artifacts map[pluginv1.ArtifactRef]pluginv1.Artifact) (Catalog, error) {
	if strings.TrimSpace(deployment.Metadata.Tenant) == "" || strings.TrimSpace(deployment.Metadata.Name) == "" || deployment.Revision == 0 {
		return Catalog{}, errors.New("配置目录要求已解析且带 tenant 的 Deployment")
	}
	deploymentDigest := deployment.Digest()
	if len(deploymentDigest) != 64 {
		return Catalog{}, errors.New("配置目录无法取得 Deployment digest")
	}
	items := make([]Definition, 0)
	for _, unit := range deployment.Units {
		installed := make([]string, 0, len(unit.Plugins))
		for _, plugin := range unit.Plugins {
			installed = append(installed, plugin.ID)
		}
		envelope, err := pluginconfig.Parse(unit.Config, installed)
		if err != nil {
			return Catalog{}, fmt.Errorf("解析 unit %s 配置信封: %w", unit.ID, err)
		}
		for _, plugin := range unit.Plugins {
			definition, configured, err := definitionFor(deployment, deploymentDigest, unit, plugin, envelope, artifacts)
			if err != nil {
				return Catalog{}, err
			}
			if configured {
				items = append(items, definition)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UnitID != items[j].UnitID {
			return items[i].UnitID < items[j].UnitID
		}
		return items[i].PluginID < items[j].PluginID
	})
	catalog := Catalog{SchemaVersion: SchemaVersion, Deployment: deployment.Metadata.Name, DeploymentRevision: deployment.Revision, DeploymentDigest: deploymentDigest, Items: items}
	digest, err := digestJSON(catalog)
	if err != nil {
		return Catalog{}, err
	}
	catalog.Digest = digest
	return catalog, catalog.Validate()
}

func definitionFor(deployment deploymentv2.Deployment, deploymentDigest string, unit deploymentv2.ServiceUnit, ref deploymentv1.PluginRef, envelope pluginconfig.Envelope, artifacts map[pluginv1.ArtifactRef]pluginv1.Artifact) (Definition, bool, error) {
	channel := normalizedChannel(ref.Channel)
	artifactRef := pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: channel}
	artifact, ok := artifacts[artifactRef]
	if !ok {
		return Definition{}, false, fmt.Errorf("配置目录缺少已验证制品 %s@%s/%s", ref.ID, ref.Version, channel)
	}
	if artifact.PluginID != ref.ID || artifact.Version != ref.Version || normalizedChannel(artifact.Channel) != channel || len(artifact.SHA256) != 64 {
		return Definition{}, false, fmt.Errorf("配置目录制品身份不一致: %s@%s/%s", ref.ID, ref.Version, channel)
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return Definition{}, false, fmt.Errorf("配置目录重新验证清单 %s: %w", ref.ID, err)
	}
	if manifest.ID != ref.ID || manifest.Version != ref.Version {
		return Definition{}, false, fmt.Errorf("配置目录清单身份不一致: %s", ref.ID)
	}
	if manifest.Configuration == nil {
		return Definition{}, false, nil
	}
	origin := deployment.Resolution.PluginOrigins[ref.ID]
	applyPath, err := applyPathFor(origin, manifest.Configuration.Scope, manifest.Configuration.ApplyMode)
	if err != nil {
		return Definition{}, false, fmt.Errorf("插件 %s 配置契约: %w", ref.ID, err)
	}
	values := envelope.Plugins[ref.ID]
	if values == nil {
		values = map[string]any{}
	}
	valuesRaw, err := json.Marshal(values)
	if err != nil {
		return Definition{}, false, fmt.Errorf("编码插件 %s 当前配置: %w", ref.ID, err)
	}
	schema := append(json.RawMessage(nil), manifest.Configuration.Schema...)
	schemaDigest, err := digestRawJSON(schema)
	if err != nil {
		return Definition{}, false, fmt.Errorf("计算插件 %s 配置 Schema 摘要: %w", ref.ID, err)
	}
	definition := Definition{
		ID: resourceID(deployment.Metadata.Tenant, deployment.Metadata.Name, unit.ID, ref.ID), Deployment: deployment.Metadata.Name,
		UnitID: unit.ID, PluginID: ref.ID, PluginName: manifest.Name, Origin: origin,
		Artifact: ArtifactIdentity{Version: ref.Version, Channel: channel, SHA256: artifact.SHA256},
		Scope:    manifest.Configuration.Scope, ApplyMode: manifest.Configuration.ApplyMode, ApplyPath: applyPath,
		Schema: schema, SchemaDigest: schemaDigest, ManagedCredentials: append([]pluginv1.ManagedCredentialField(nil), manifest.Configuration.ManagedCredentials...),
		Values: valuesRaw, DeploymentRevision: deployment.Revision, DeploymentDigest: deploymentDigest,
	}
	if err := ValidateValues(definition, valuesRaw); err != nil {
		return Definition{}, false, fmt.Errorf("插件 %s 当前配置不符合签名 Schema: %w", ref.ID, err)
	}
	return definition, true, nil
}

func applyPathFor(origin, scope, mode string) (ApplyPath, error) {
	switch mode {
	case "restart":
		if scope != "service" {
			return "", errors.New("restart 只允许 service scope")
		}
		switch origin {
		case deploymentv2.OriginApplication:
			return ApplyApplicationDeployment, nil
		case deploymentv2.OriginPlatformProfile:
			return ApplyPlatformProfile, nil
		default:
			return "", errors.New("restart 配置缺少可信插件来源")
		}
	case "hot":
		switch scope {
		case "service":
			return ApplyHotService, nil
		case "tenant", "user":
			return ApplyHotScoped, nil
		default:
			return "", errors.New("hot 配置 scope 无效")
		}
	default:
		return "", errors.New("配置 applyMode 无效")
	}
}

// Validate rejects catalogs that were truncated or assembled from mismatched
// deployment generations. It does not re-establish artifact trust by itself.
func (c Catalog) Validate() error {
	if c.SchemaVersion != SchemaVersion || strings.TrimSpace(c.Deployment) == "" || c.DeploymentRevision == 0 || len(c.DeploymentDigest) != 64 || len(c.Digest) != 64 {
		return errors.New("配置目录身份无效")
	}
	seen := map[string]struct{}{}
	for _, item := range c.Items {
		if _, duplicate := seen[item.ID]; duplicate {
			return fmt.Errorf("配置目录资源 ID 重复: %s", item.ID)
		}
		seen[item.ID] = struct{}{}
		if !strings.HasPrefix(item.ID, "cfg_") || len(item.ID) != 28 || item.Deployment != c.Deployment || item.DeploymentRevision != c.DeploymentRevision || item.DeploymentDigest != c.DeploymentDigest {
			return fmt.Errorf("配置目录项身份无效: %s", item.ID)
		}
		if strings.TrimSpace(item.UnitID) == "" || strings.TrimSpace(item.PluginID) == "" || strings.TrimSpace(item.PluginName) == "" || len(item.Artifact.SHA256) != 64 || len(item.SchemaDigest) != 64 || !json.Valid(item.Schema) || !json.Valid(item.Values) {
			return fmt.Errorf("配置目录项内容无效: %s", item.ID)
		}
		if expected, err := digestRawJSON(item.Schema); err != nil || expected != item.SchemaDigest {
			return fmt.Errorf("配置目录项 Schema 摘要无效: %s", item.ID)
		}
		if item.Origin != deploymentv2.OriginApplication && item.Origin != deploymentv2.OriginPlatformProfile {
			return fmt.Errorf("配置目录项来源无效: %s", item.ID)
		}
		expectedPath, err := applyPathFor(item.Origin, item.Scope, item.ApplyMode)
		if err != nil || expectedPath != item.ApplyPath {
			return fmt.Errorf("配置目录项生效路径无效: %s", item.ID)
		}
	}
	copy := c
	copy.Digest = ""
	expected, err := digestJSON(copy)
	if err != nil || expected != c.Digest {
		return errors.New("配置目录摘要无效")
	}
	return nil
}

func resourceID(tenant, deployment, unit, plugin string) string {
	hash := sha256.New()
	for _, value := range []string{tenant, deployment, unit, plugin} {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(value), value)
	}
	return "cfg_" + hex.EncodeToString(hash.Sum(nil))[:24]
}

func digestRawJSON(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return digestJSON(value)
}

func digestJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func normalizedChannel(channel string) string {
	if channel == "" {
		return "stable"
	}
	return channel
}
