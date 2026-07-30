// Command artifactrepository 启动 HTTPS 制品仓库基础插件进程。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactreport"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository/localtest"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactstorage"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const pluginID = "cn.vastplan.platform.artifacts.repository"

// pluginVersion defaults to the checked-in manifest version for go test/go run.
// Production and development builds inject the manifest value from build.sh,
// keeping the packaged binary and signed manifest on the same version source.
var pluginVersion = "0.35.6"

var runtimeRepositoryDescriptor = []byte(`{"title":"制品仓库","subcommands":[{"name":"status","description":"读取仓库运行状态"},{"name":"assessmentInventory","description":"读取评估数据库 revision 与报告归档状态"},{"name":"prepareAssessmentReport","description":"验证原始报告仍被当前评估证据引用"},{"name":"capacity","description":"读取已验证容量与配额用量"},{"name":"listCatalog","description":"分页查询已验证制品目录"},{"name":"listPublishJournal","description":"按 revision 查询发布流水账"},{"name":"resolve","description":"生成精确依赖锁"},{"name":"describePlanning","description":"读取精确制品的已验签规划描述"},{"name":"setLifecycle","description":"以 CAS 更新制品生命周期"},{"name":"putReferences","description":"发布完整制品引用快照"},{"name":"listReferences","description":"读取制品引用保护状态"},{"name":"gcPlan","description":"生成无副作用 GC 计划"},{"name":"gcStatus","description":"读取隔离与清扫状态"},{"name":"gcQuarantine","description":"按精确计划隔离制品"},{"name":"gcSweep","description":"复核并清扫过期隔离制品"},{"name":"migrationStatus","description":"读取迁移状态"},{"name":"prepareMigration","description":"准备候选 volume"},{"name":"syncMigration","description":"追平候选 volume"},{"name":"cutoverMigration","description":"原子切换候选 volume"},{"name":"rollbackMigration","description":"回滚到源 volume"},{"name":"finalizeMigration","description":"结束观察双写"},{"name":"releaseMigration","description":"隔离旧 volume"},{"name":"listPublications","description":"读取 stable 发布审批"},{"name":"submitPublication","description":"提交 testing 到 stable 发布审批"},{"name":"approvePublication","description":"以双人分离批准 stable 发布"},{"name":"rejectPublication","description":"驳回或撤回 stable 发布批准"},{"name":"cancelPublication","description":"由原提交人撤销 stable 发布申请"},{"name":"getSupplyChainEvidence","description":"读取已验证供应链证据摘要"},{"name":"prepareAssessment","description":"向精确首方扫描 Provider 签发一次性制品读取租约"},{"name":"appendAssessmentStatus","description":"由精确 Controller 追加 Provider 签名复扫状态"},{"name":"installDataPlaneTicket","description":"安装控制面签发的一次性制品 Ticket"},{"name":"installAssessmentReportTicket","description":"安装控制面签发的一次性评估报告 Ticket"}]}`)

type serverConfig struct {
	profile                                                                                                                              artifactrepositoryv1.Profile
	addr, repository, storageProvider, volumeID, migrationState, trust, cert, key, readToken, publishToken, bundleToken, assessmentToken string
	localToken                                                                                                                           string
	assessmentReports                                                                                                                    string
	quota                                                                                                                                repositoryruntime.QuotaPolicy
	publication                                                                                                                          repositoryruntime.PublicationPolicy
	supplyChain                                                                                                                          repositoryruntime.SupplyChainPolicy
	apiExposure                                                                                                                          *dataPlaneLeaseConfig
}

func loadConfig() (serverConfig, error) {
	var startup struct {
		RepositoryProfile artifactrepositoryv1.Profile        `json:"repositoryProfile"`
		Listen            string                              `json:"listen"`
		StorageProvider   string                              `json:"storageProvider"`
		VolumeID          string                              `json:"volumeId"`
		Quota             repositoryruntime.QuotaPolicy       `json:"quota"`
		Publication       repositoryruntime.PublicationPolicy `json:"publication"`
		SupplyChain       repositoryruntime.SupplyChainPolicy `json:"supplyChain"`
		APIExposure       *dataPlaneLeaseConfig               `json:"apiExposure,omitempty"`
	}
	if err := sdk.DecodeStartupConfiguration(&startup); err != nil {
		return serverConfig{}, err
	}
	config := serverConfig{
		profile:           startup.RepositoryProfile,
		repository:        os.Getenv("VASTPLAN_ARTIFACT_REPOSITORY"),
		storageProvider:   startup.StorageProvider,
		volumeID:          startup.VolumeID,
		migrationState:    os.Getenv("VASTPLAN_ARTIFACT_MIGRATION_STATE"),
		trust:             os.Getenv("VASTPLAN_ARTIFACT_TRUST"),
		cert:              os.Getenv("VASTPLAN_ARTIFACT_TLS_CERT"),
		key:               os.Getenv("VASTPLAN_ARTIFACT_TLS_KEY"),
		readToken:         os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN"),
		publishToken:      os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN"),
		bundleToken:       os.Getenv("VASTPLAN_ARTIFACT_BUNDLE_TOKEN"),
		assessmentToken:   os.Getenv("VASTPLAN_ARTIFACT_ASSESSMENT_TOKEN"),
		assessmentReports: os.Getenv("VASTPLAN_ARTIFACT_ASSESSMENT_REPORTS"),
		quota:             startup.Quota,
		publication:       startup.Publication,
		supplyChain:       startup.SupplyChain,
		apiExposure:       startup.APIExposure,
	}
	validatedProfile, err := artifactrepositoryv1.ValidateProfile(config.profile)
	if err != nil {
		return config, fmt.Errorf("制品仓库 Repository Profile: %w", err)
	}
	config.profile = validatedProfile
	endpoint, err := url.Parse(config.profile.Endpoint)
	if err != nil {
		return config, err
	}
	if config.profile.Protocol == artifactrepositoryv1.ProtocolRemote {
		config.addr = strings.TrimSpace(startup.Listen)
		if config.addr == "" {
			return config, errors.New("remote 制品仓库必须配置独立 listen 地址")
		}
	} else {
		config.addr = endpoint.Path
		config.localToken, err = localtest.ReadTokenFile(os.Getenv("VASTPLAN_ARTIFACT_LOCAL_TOKEN_FILE"))
		if err != nil {
			return config, err
		}
	}
	if config.storageProvider == "" {
		config.storageProvider = "platform.artifacts.storage.file"
	}
	if config.volumeID == "" {
		config.volumeID = "repository.primary"
	}
	if err := artifactstorage.ValidateProviderID(config.storageProvider); err != nil {
		return config, err
	}
	if err := artifactstorage.ValidateVolumeID(config.volumeID); err != nil {
		return config, err
	}
	if config.repository == "" || config.assessmentReports == "" || config.migrationState == "" || config.trust == "" {
		return config, errors.New("制品仓库必须配置存储、信任文档、评估归档和迁移状态")
	}
	if config.profile.Protocol == artifactrepositoryv1.ProtocolRemote && (config.cert == "" || config.key == "" || config.readToken == "" || config.publishToken == "" || config.bundleToken == "" || config.assessmentToken == "" || config.readToken == config.publishToken || config.readToken == config.bundleToken || config.readToken == config.assessmentToken || config.publishToken == config.bundleToken || config.publishToken == config.assessmentToken || config.bundleToken == config.assessmentToken) {
		return config, errors.New("remote 制品仓库必须配置 TLS 和互不相同的读取/发布/Bundle/Assessment 令牌")
	}
	if config.profile.Protocol == artifactrepositoryv1.ProtocolLocalTest && config.apiExposure != nil {
		return config, errors.New("local-test 制品仓库不得注册生产数据面 Exposure")
	}
	if !filepath.IsAbs(config.assessmentReports) || filepath.Clean(config.assessmentReports) != config.assessmentReports || pathsOverlap(config.repository, config.assessmentReports) {
		return config, errors.New("安全评估报告归档必须是与制品 volume 隔离的规范绝对路径")
	}
	if err := validateDataPlaneLeaseConfig(config.apiExposure); err != nil {
		return config, err
	}
	return config, nil
}

func pathsOverlap(left, right string) bool {
	for _, pair := range [][2]string{{left, right}, {right, left}} {
		relative, err := filepath.Rel(filepath.Clean(pair[0]), filepath.Clean(pair[1]))
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	trust, err := artifactrepository.LoadTrustStore(config.trust)
	if err != nil {
		log.Fatal(err)
	}
	trustRaw, err := os.ReadFile(config.trust)
	if err != nil {
		log.Fatalf("读取制品信任快照失败: %v", err)
	}
	reportArchive, err := artifactreport.New(config.assessmentReports)
	if err != nil {
		log.Fatalf("打开安全评估报告归档失败: %v", err)
	}
	manager, err := repositoryruntime.Open(artifactstorage.Volume{
		Handle: "artifact-storage://configured", ProviderID: config.storageProvider, VolumeID: config.volumeID,
		AccessMode: "filesystem", MountPath: config.repository, Generation: 1, Ready: true,
	}, trust, config.migrationState, repositoryruntime.Options{Quota: config.quota, Publication: config.publication, SupplyChain: config.supplyChain, AssessmentReports: reportArchive})
	if err != nil {
		log.Fatalf("打开可迁移制品仓库失败: %v", err)
	}
	transport, err := startRepositoryTransport(config, manager, trustRaw)
	if err != nil {
		log.Fatalf("启动制品仓库传输失败: %v", err)
	}
	p := sdk.New(pluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	leaseRegistrar := &dataPlaneLeaseRegistrar{config: config.apiExposure}
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             "platform.artifacts.repository",
		Descriptor:     runtimeRepositoryDescriptor,
		Handlers:       repositoryHandlers(config, manager, transport, leaseRegistrar),
	})

	if err := p.Serve(); err != nil {
		log.Printf("制品仓库插件退出: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = transport.Shutdown(shutdownCtx)
}

func referenceOwnerAllowed(callerID, ownerKind string) bool {
	switch callerID {
	case "cn.vastplan.platform.infrastructure.deployment-manager":
		return ownerKind == "deployment-active" || ownerKind == "artifact-lock" || ownerKind == "rollback-history"
	case "cn.vastplan.platform.configuration.portal-composer":
		return ownerKind == "portal-activation" || ownerKind == "artifact-lock" || ownerKind == "rollback-history"
	default:
		if strings.HasPrefix(callerID, "node-agent/") {
			return ownerKind == "assignment-active"
		}
		return strings.HasPrefix(callerID, "bootstrap-inventory/") && (ownerKind == "seed" || ownerKind == "last-known-good")
	}
}

func referenceOwnerIDAllowed(callerID, ownerKind, ownerID string) bool {
	switch callerID {
	case "cn.vastplan.platform.infrastructure.deployment-manager":
		return strings.HasPrefix(ownerID, "deployment/")
	case "cn.vastplan.platform.configuration.portal-composer":
		return strings.HasPrefix(ownerID, "portal/")
	default:
		if repositoryID := strings.TrimPrefix(callerID, "bootstrap-inventory/"); repositoryID != callerID {
			return repositoryID != "" && ((ownerKind == "seed" && ownerID == "seed/"+repositoryID) || (ownerKind == "last-known-good" && ownerID == "lkg/"+repositoryID))
		}
		nodeID := strings.TrimPrefix(callerID, "node-agent/")
		return ownerKind == "assignment-active" && nodeID != "" && strings.HasPrefix(ownerID, "assignment/") && strings.HasSuffix(ownerID, "/"+nodeID)
	}
}

func decodeParams(raw []byte, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析仓库查询参数: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("仓库查询参数只能包含一个 JSON 对象")
		}
		return err
	}
	return nil
}
