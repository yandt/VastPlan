package artifactrepository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactreport"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

const remoteCatalogPageSize = 100

// RemoteAdapter projects the managed repository HTTPS API onto the exact
// artifact.repository.remote.v1 contract used by Local Plugin Library sync.
type RemoteAdapter struct {
	profile    artifactrepositoryv1.Profile
	repository *RemoteRepository
}

func NewRemoteAdapter(profile artifactrepositoryv1.Profile, repository *RemoteRepository) (*RemoteAdapter, error) {
	profile, err := artifactrepositoryv1.ValidateProfile(profile)
	if err != nil {
		return nil, err
	}
	if profile.Protocol != artifactrepositoryv1.ProtocolRemote || repository == nil {
		return nil, errors.New("remote Adapter 配置无效")
	}
	if strings.TrimRight(strings.TrimSpace(repository.BaseURL), "/") != strings.TrimRight(profile.Endpoint, "/") {
		return nil, errors.New("remote Adapter URL 与 Profile endpoint 不一致")
	}
	if _, _, _, err := repository.validate(); err != nil {
		return nil, err
	}
	return &RemoteAdapter{profile: profile, repository: repository}, nil
}

func (a *RemoteAdapter) Profile() artifactrepositoryv1.Profile { return a.profile }

func (a *RemoteAdapter) ReadExact(ctx context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	if err := artifactrepositoryv1.ValidateRef(a.profile, ref); err != nil {
		return artifacttrust.Envelope{}, err
	}
	return a.repository.Fetch(ctx, ref)
}

func (a *RemoteAdapter) ReadAssessmentReport(ctx context.Context, digest string) ([]byte, error) {
	if !commonv1.IsSHA256(digest) {
		return nil, errors.New("安全评估报告摘要无效")
	}
	base, client, _, err := a.repository.validate()
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(base.String(), "/") + "/v1/assessment-reports/" + digest
	raw, err := a.repository.get(ctx, client, endpoint, artifactreport.MaxBytes, false)
	if err != nil {
		return nil, err
	}
	actual := sha256.Sum256(raw)
	if len(raw) == 0 || hex.EncodeToString(actual[:]) != digest {
		return nil, errors.New("远端安全评估报告与摘要不一致")
	}
	return raw, nil
}

func (a *RemoteAdapter) Publish(ctx context.Context, envelope artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	ref := pluginv1.ArtifactRef{PluginID: envelope.Artifact.PluginID, Version: envelope.Artifact.Version, Channel: envelope.Artifact.Channel}
	if err := artifactrepositoryv1.ValidatePublishRef(a.profile, ref); err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	if len(envelope.SecurityStatusChain) != 0 {
		return artifactrepositoryv1.Receipt{}, errors.New("remote 普通发布不得覆盖追加式 security status chain")
	}
	var attestation Attestation
	if err := decodeJSONStrict(envelope.Proof, &attestation); err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	published, err := a.repository.PublishRemoteWithSupplyChain(ctx, attestation, envelope.PackageBytes, envelope.Provenance, envelope.ProvenanceVerification, envelope.SecurityAdmission)
	if err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	if published.PluginID != ref.PluginID || published.Version != ref.Version || published.Channel != ref.Channel || published.SHA256 != envelope.Artifact.SHA256 {
		return artifactrepositoryv1.Receipt{}, errors.New("remote 发布响应与请求不一致")
	}
	snapshot, err := a.CatalogSnapshot(ctx)
	if err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	for _, receipt := range snapshot.Items {
		if receipt.Ref == ref {
			if receipt.SHA256 != envelope.Artifact.SHA256 {
				return artifactrepositoryv1.Receipt{}, errors.New("remote 发布后 Catalog 摘要漂移")
			}
			return receipt, nil
		}
	}
	return artifactrepositoryv1.Receipt{}, errors.New("remote 发布后 Catalog 缺少精确引用")
}

func (a *RemoteAdapter) CatalogSnapshot(ctx context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	items := make([]artifactrepositoryv1.Receipt, 0)
	var snapshotRevision uint64
	for pageNumber := 1; ; pageNumber++ {
		page, err := a.catalogPage(ctx, pageNumber)
		if err != nil {
			return artifactrepositoryv1.CatalogSnapshot{}, err
		}
		if pageNumber == 1 {
			snapshotRevision = page.Revision
		} else if page.Revision != snapshotRevision {
			return artifactrepositoryv1.CatalogSnapshot{}, errors.New("remote Catalog 分页期间 revision 变化，请重试")
		}
		for _, entry := range page.Items {
			if entry.LifecycleStatus == "yanked" || entry.LifecycleStatus == "revoked" {
				continue
			}
			if entry.LifecycleStatus != "" && entry.LifecycleStatus != "active" && entry.LifecycleStatus != "deprecated" {
				return artifactrepositoryv1.CatalogSnapshot{}, errors.New("remote Catalog 返回未知生命周期状态")
			}
			if artifactrepositoryv1.ValidateRef(a.profile, entry.Ref) != nil {
				continue
			}
			receipt := artifactrepositoryv1.Receipt{
				SchemaVersion: artifactrepositoryv1.ProfileVersion,
				RepositoryID:  a.profile.ID, Protocol: a.profile.Protocol, ProfileDigest: a.profile.Digest(),
				Ref: entry.Ref, SHA256: entry.SHA256, Revision: entry.RepositoryRevision,
			}
			if err := artifactrepositoryv1.ValidateReceipt(a.profile, receipt); err != nil {
				return artifactrepositoryv1.CatalogSnapshot{}, fmt.Errorf("remote Catalog 返回无效条目: %w", err)
			}
			items = append(items, receipt)
		}
		if page.Page*page.PageSize >= page.Total {
			break
		}
		if pageNumber >= 1_000_000 {
			return artifactrepositoryv1.CatalogSnapshot{}, errors.New("remote Catalog 分页数量超限")
		}
	}
	snapshot := artifactrepositoryv1.CatalogSnapshot{
		SchemaVersion: artifactrepositoryv1.ProfileVersion,
		RepositoryID:  a.profile.ID, Protocol: a.profile.Protocol, ProfileDigest: a.profile.Digest(),
		Revision: snapshotRevision, Items: items,
	}
	if err := artifactrepositoryv1.ValidateCatalogSnapshot(a.profile, snapshot); err != nil {
		return artifactrepositoryv1.CatalogSnapshot{}, err
	}
	return snapshot, nil
}

func (a *RemoteAdapter) ResolveLock(ctx context.Context, input pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if err := pluginv1.ValidateArtifactResolveRequest(raw); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	base, client, _, err := a.repository.validate()
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base.String(), "/")+"/v1/catalog/resolve", bytes.NewReader(raw))
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	setBearer(request, a.repository.Token)
	response, err := client.Do(request)
	if err != nil {
		return pluginv1.ArtifactLock{}, fmt.Errorf("调用 remote Catalog Resolver: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return pluginv1.ArtifactLock{}, fmt.Errorf("remote Catalog Resolver 返回 %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	responseRaw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(responseRaw) > 2<<20 {
		return pluginv1.ArtifactLock{}, errors.New("remote Artifact Lock 响应无效或超限")
	}
	if err := pluginv1.ValidateArtifactLock(responseRaw); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	var lock pluginv1.ArtifactLock
	if err := json.Unmarshal(responseRaw, &lock); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	digest, err := pluginv1.ArtifactLockDigest(lock)
	if err != nil || digest != lock.Digest {
		return pluginv1.ArtifactLock{}, errors.New("remote Artifact Lock digest 无效")
	}
	return lock, nil
}

type remoteCatalogPage struct {
	Revision uint64               `json:"revision"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
	Items    []remoteCatalogEntry `json:"items"`
}

type remoteCatalogEntry struct {
	Ref                pluginv1.ArtifactRef `json:"ref"`
	SHA256             string               `json:"sha256"`
	RepositoryRevision uint64               `json:"repositoryRevision"`
	LifecycleStatus    string               `json:"lifecycleStatus"`
}

func (a *RemoteAdapter) catalogPage(ctx context.Context, page int) (remoteCatalogPage, error) {
	base, client, _, err := a.repository.validate()
	if err != nil {
		return remoteCatalogPage{}, err
	}
	endpoint := strings.TrimRight(base.String(), "/") + "/v1/catalog/artifacts"
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("pageSize", strconv.Itoa(remoteCatalogPageSize))
	raw, err := a.repository.get(ctx, client, endpoint+"?"+query.Encode(), 16<<20, false)
	if err != nil {
		return remoteCatalogPage{}, err
	}
	var result remoteCatalogPage
	if err := json.Unmarshal(raw, &result); err != nil {
		return remoteCatalogPage{}, fmt.Errorf("解析 remote Catalog: %w", err)
	}
	if result.Revision == 0 && result.Total != 0 || result.Total < 0 || result.Page != page || result.PageSize <= 0 || result.PageSize > remoteCatalogPageSize || len(result.Items) > result.PageSize {
		return remoteCatalogPage{}, errors.New("remote Catalog 分页响应无效")
	}
	return result, nil
}
