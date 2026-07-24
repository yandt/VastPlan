package portaltrust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// TestArtifactIndex is a trusted-host receipt verifier, not a generic catalog
// lookup. Implementations must bind the caller's complete receipt to the active
// Repository Profile and current immutable catalog.
type TestArtifactIndex interface {
	Validate(context.Context, artifactrepositoryv1.Receipt) error
}

type LocalTestArtifactIndex struct{ Adapter artifactrepository.Adapter }

func (i LocalTestArtifactIndex) Validate(ctx context.Context, receipt artifactrepositoryv1.Receipt) error {
	if i.Adapter == nil {
		return errors.New("Portal local-test 制品索引未配置")
	}
	profile := i.Adapter.Profile()
	if err := artifactrepositoryv1.ValidateReceipt(profile, receipt); err != nil {
		return err
	}
	snapshot, err := i.Adapter.CatalogSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("读取 Portal local-test Catalog: %w", err)
	}
	for _, current := range snapshot.Items {
		if sameReceipt(current, receipt) {
			return nil
		}
	}
	return errors.New("Portal local-test 回执已过期、被撤销或不属于当前 Profile")
}

func sameReceipt(left, right artifactrepositoryv1.Receipt) bool {
	if left.SchemaVersion != right.SchemaVersion || left.RepositoryID != right.RepositoryID || left.Protocol != right.Protocol || left.ProfileDigest != right.ProfileDigest || left.Ref != right.Ref || !strings.EqualFold(left.SHA256, right.SHA256) || left.Revision != right.Revision || left.WorkspaceLease != right.WorkspaceLease {
		return false
	}
	if left.ExpiresAt == nil || right.ExpiresAt == nil {
		return left.ExpiresAt == nil && right.ExpiresAt == nil
	}
	return left.ExpiresAt.Equal(*right.ExpiresAt)
}

type RemoteTestArtifactIndex struct {
	Profile artifactrepositoryv1.Profile
	BaseURL string
	Token   string
	Client  *http.Client
}

func (i RemoteTestArtifactIndex) Validate(ctx context.Context, receipt artifactrepositoryv1.Receipt) error {
	profile, err := artifactrepositoryv1.ValidateProfile(i.Profile)
	if err != nil || profile.Protocol != artifactrepositoryv1.ProtocolRemote || artifactrepositoryv1.ValidateReceipt(profile, receipt) != nil {
		return errors.New("Portal 远端测试仓库回执与活动 Profile 不匹配")
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(i.BaseURL), "/"))
	if err != nil || base.Scheme != "https" || base.Host == "" || i.Client == nil || strings.TrimSpace(i.Token) == "" || base.String() != strings.TrimRight(profile.Endpoint, "/") {
		return errors.New("Portal 测试制品远端索引配置无效")
	}
	query := url.Values{
		"pluginId": {receipt.Ref.PluginID}, "version": {receipt.Ref.Version}, "channel": {receipt.Ref.Channel},
		"target": {"frontend"}, "page": {"1"}, "pageSize": {"2"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/v1/catalog/artifacts?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+i.Token)
	response, err := i.Client.Do(request)
	if err != nil {
		return fmt.Errorf("读取 Portal 测试制品目录: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("Portal 测试制品目录返回 %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	var page struct {
		Total int `json:"total"`
		Items []struct {
			Ref                pluginv1.ArtifactRef `json:"ref"`
			SHA256             string               `json:"sha256"`
			RepositoryRevision uint64               `json:"repositoryRevision"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&page); err != nil {
		return fmt.Errorf("解析 Portal 测试制品目录: %w", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || !strings.EqualFold(page.Items[0].SHA256, receipt.SHA256) || page.Items[0].RepositoryRevision != receipt.Revision {
		return errors.New("Portal 测试制品与远端仓库精确回执不一致")
	}
	if page.Items[0].Ref != receipt.Ref {
		return errors.New("Portal 测试制品目录未返回唯一精确 ref")
	}
	return nil
}

func (c *TrustedCatalog) ValidateTestArtifact(ctx context.Context, tenantID string, request portalapi.CreateTestReleaseRequest, allowedPublishers []string) error {
	if c == nil || c.testIndex == nil || strings.TrimSpace(tenantID) == "" {
		return errors.New("Portal 测试制品验证上下文不完整")
	}
	ref := request.Receipt.Ref
	if ref.Channel != "testing" && ref.Channel != "workspace" {
		return errors.New("Portal 测试制品 channel 无效")
	}
	pluginRef := portalapi.PluginRef{ID: ref.PluginID, Version: ref.Version, Channel: ref.Channel}
	artifact, _, manifest, err := c.verifiedManifest(ctx, pluginRef)
	if err != nil {
		return err
	}
	if !strings.EqualFold(artifact.SHA256, request.Receipt.SHA256) || manifest.Engines["frontend"] == "" || !containsText(allowedPublishers, manifest.Publisher) {
		return errors.New("Portal 测试制品内容、目标或发布者不符合绑定")
	}
	if err := c.testIndex.Validate(ctx, request.Receipt); err != nil {
		return err
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
