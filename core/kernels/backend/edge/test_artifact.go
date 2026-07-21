package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type TestArtifactIndexEntry struct {
	Ref                pluginv1.ArtifactRef `json:"ref"`
	SHA256             string               `json:"sha256"`
	Publisher          string               `json:"publisher"`
	RepositoryRevision uint64               `json:"repositoryRevision"`
	Targets            []string             `json:"targets"`
}

type TestArtifactIndex interface {
	Lookup(context.Context, pluginv1.ArtifactRef) (TestArtifactIndexEntry, error)
}

type RemoteTestArtifactIndex struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (i RemoteTestArtifactIndex) Lookup(ctx context.Context, ref pluginv1.ArtifactRef) (TestArtifactIndexEntry, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(i.BaseURL), "/"))
	if err != nil || base.Scheme != "https" || base.Host == "" || i.Client == nil || strings.TrimSpace(i.Token) == "" {
		return TestArtifactIndexEntry{}, errors.New("Portal 测试制品远端索引配置无效")
	}
	query := url.Values{
		"pluginId": {ref.PluginID}, "version": {ref.Version}, "channel": {ref.Channel},
		"target": {"frontend"}, "page": {"1"}, "pageSize": {"2"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/v1/catalog/artifacts?"+query.Encode(), nil)
	if err != nil {
		return TestArtifactIndexEntry{}, err
	}
	request.Header.Set("Authorization", "Bearer "+i.Token)
	response, err := i.Client.Do(request)
	if err != nil {
		return TestArtifactIndexEntry{}, fmt.Errorf("读取 Portal 测试制品目录: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return TestArtifactIndexEntry{}, fmt.Errorf("Portal 测试制品目录返回 %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	var page struct {
		Total int                      `json:"total"`
		Items []TestArtifactIndexEntry `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&page); err != nil {
		return TestArtifactIndexEntry{}, fmt.Errorf("解析 Portal 测试制品目录: %w", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Ref != ref {
		return TestArtifactIndexEntry{}, errors.New("Portal 测试制品目录未返回唯一精确 ref")
	}
	return page.Items[0], nil
}

func (c *TrustedCatalog) ValidateTestArtifact(ctx context.Context, tenantID string, request portalapi.CreateTestReleaseRequest, allowedPublishers []string) error {
	if c == nil || c.testIndex == nil || strings.TrimSpace(tenantID) == "" || request.Artifact.Channel != "testing" || request.RepositoryRevision == 0 {
		return errors.New("Portal 测试制品验证上下文不完整")
	}
	ref := portalapi.PluginRef{ID: request.Artifact.PluginID, Version: request.Artifact.Version, Channel: request.Artifact.Channel}
	artifact, _, manifest, err := c.verifiedManifest(ctx, ref)
	if err != nil {
		return err
	}
	if !strings.EqualFold(artifact.SHA256, request.SHA256) || manifest.Engines["frontend"] == "" || !containsText(allowedPublishers, manifest.Publisher) {
		return errors.New("Portal 测试制品内容、目标或发布者不符合绑定")
	}
	entry, err := c.testIndex.Lookup(ctx, request.Artifact)
	if err != nil {
		return err
	}
	if !strings.EqualFold(entry.SHA256, request.SHA256) || entry.RepositoryRevision != request.RepositoryRevision || entry.Publisher != manifest.Publisher || !containsText(entry.Targets, "frontend") {
		return errors.New("Portal 测试制品与仓库精确回执不一致")
	}
	return nil
}

func containsText(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
