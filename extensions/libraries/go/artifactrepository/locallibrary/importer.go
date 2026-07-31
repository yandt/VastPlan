// Package locallibrary synchronizes exact, already cataloged remote artifacts
// into a Local Plugin Library. It coordinates protocol adapters only; trust and
// immutable storage remain enforced by the destination repository.
package locallibrary

import (
	"context"
	"errors"
	"fmt"
	"sort"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

// ImportExact re-reads the source Catalog before downloading the object. A
// journal notification is only a trigger and can never supply trusted fields.
func ImportExact(ctx context.Context, source artifactrepository.Adapter, destination artifactrepository.ImportAdapter, ref pluginv1.ArtifactRef) (artifactrepositoryv1.ImportRecord, error) {
	if source == nil || destination == nil {
		return artifactrepositoryv1.ImportRecord{}, errors.New("插件导入必须配置源仓库与 Local Plugin Library")
	}
	sourceProfile, destinationProfile := source.Profile(), destination.Profile()
	if sourceProfile.Protocol != artifactrepositoryv1.ProtocolRemote || destinationProfile.Protocol != artifactrepositoryv1.ProtocolLocalTest {
		return artifactrepositoryv1.ImportRecord{}, errors.New("插件导入只允许 remote.v1 到 local-test.v1")
	}
	if err := artifactrepositoryv1.ValidateRef(sourceProfile, ref); err != nil {
		return artifactrepositoryv1.ImportRecord{}, fmt.Errorf("远端引用不属于源 Profile: %w", err)
	}
	if err := artifactrepositoryv1.ValidateRef(destinationProfile, ref); err != nil {
		return artifactrepositoryv1.ImportRecord{}, fmt.Errorf("远端引用不属于本地库 Profile: %w", err)
	}
	if ref.Channel == "workspace" {
		return artifactrepositoryv1.ImportRecord{}, errors.New("远端仓库不得导入 workspace 制品")
	}
	snapshot, err := source.CatalogSnapshot(ctx)
	if err != nil {
		return artifactrepositoryv1.ImportRecord{}, fmt.Errorf("读取远端 Catalog: %w", err)
	}
	var receipt artifactrepositoryv1.Receipt
	found := false
	for _, candidate := range snapshot.Items {
		if candidate.Ref != ref {
			continue
		}
		if found {
			return artifactrepositoryv1.ImportRecord{}, errors.New("远端 Catalog 包含重复精确引用")
		}
		receipt, found = candidate, true
	}
	if !found {
		return artifactrepositoryv1.ImportRecord{}, errors.New("远端 Catalog 不包含待导入精确引用")
	}
	envelope, err := source.ReadExact(ctx, ref)
	if err != nil {
		return artifactrepositoryv1.ImportRecord{}, fmt.Errorf("下载远端精确制品: %w", err)
	}
	if envelope.Artifact.PluginID != ref.PluginID || envelope.Artifact.Version != ref.Version || envelope.Artifact.Channel != ref.Channel || envelope.Artifact.SHA256 != receipt.SHA256 {
		return artifactrepositoryv1.ImportRecord{}, errors.New("远端制品与 Catalog 回执不一致")
	}
	if err := importAssessmentReports(ctx, source, destination, envelope); err != nil {
		return artifactrepositoryv1.ImportRecord{}, err
	}
	record, err := destination.ImportExact(ctx, sourceProfile, receipt, envelope)
	if err != nil {
		return artifactrepositoryv1.ImportRecord{}, err
	}
	if err := artifactrepositoryv1.ValidateImportRecord(sourceProfile, destinationProfile, record); err != nil {
		return artifactrepositoryv1.ImportRecord{}, fmt.Errorf("Local Plugin Library 返回无效导入记录: %w", err)
	}
	return record, nil
}

// ImportLock imports the complete exact dependency closure selected by the
// remote resolver. Each immutable object is independently idempotent; callers
// must only expose the lock for activation after every record succeeds.
func ImportLock(ctx context.Context, source artifactrepository.LockResolver, destination artifactrepository.ImportAdapter, lock pluginv1.ArtifactLock) ([]artifactrepositoryv1.ImportRecord, error) {
	if source == nil || destination == nil {
		return nil, errors.New("锁导入必须配置远端 Resolver 与 Local Plugin Library")
	}
	if err := pluginv1.ValidateArtifactLockSemantics(lock); err != nil {
		return nil, err
	}
	snapshot, err := source.CatalogSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取远端 Catalog: %w", err)
	}
	if lock.RepositoryRevision == 0 || lock.RepositoryRevision > snapshot.Revision {
		return nil, errors.New("Artifact Lock revision 不属于当前远端 Catalog")
	}
	receipts := make(map[pluginv1.ArtifactRef]artifactrepositoryv1.Receipt, len(snapshot.Items))
	for _, receipt := range snapshot.Items {
		receipts[receipt.Ref] = receipt
	}
	records := make([]artifactrepositoryv1.ImportRecord, 0, len(lock.Packages))
	seen := make(map[string]struct{}, len(lock.Packages))
	for _, item := range lock.Packages {
		if _, duplicate := seen[item.Ref.PluginID]; duplicate || item.RepositoryRevision > lock.RepositoryRevision {
			return nil, errors.New("Artifact Lock 包身份重复或 revision 越界")
		}
		seen[item.Ref.PluginID] = struct{}{}
		receipt, ok := receipts[item.Ref]
		if !ok || receipt.SHA256 != item.SHA256 || receipt.Revision != item.RepositoryRevision {
			return nil, fmt.Errorf("远端 Catalog 与 Artifact Lock 不一致: %s", item.Ref.PluginID)
		}
		envelope, err := source.ReadExact(ctx, item.Ref)
		if err != nil {
			return nil, fmt.Errorf("下载锁定制品 %s: %w", item.Ref.PluginID, err)
		}
		if envelope.Artifact.SHA256 != item.SHA256 || envelope.Artifact.Size != item.Size {
			return nil, fmt.Errorf("锁定制品内容漂移: %s", item.Ref.PluginID)
		}
		if err := importAssessmentReports(ctx, source, destination, envelope); err != nil {
			return nil, fmt.Errorf("导入锁定制品 %s 的安全评估报告: %w", item.Ref.PluginID, err)
		}
		record, err := destination.ImportExact(ctx, source.Profile(), receipt, envelope)
		if err != nil {
			return nil, fmt.Errorf("导入锁定制品 %s: %w", item.Ref.PluginID, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func importAssessmentReports(ctx context.Context, source artifactrepository.Adapter, destination artifactrepository.ImportAdapter, envelope artifacttrust.Envelope) error {
	digests, err := assessmentReportDigests(envelope)
	if err != nil || len(digests) == 0 {
		return err
	}
	reportSource, sourceOK := source.(artifactrepository.AssessmentReportSource)
	reportDestination, destinationOK := destination.(artifactrepository.AssessmentReportImporter)
	if !sourceOK || !destinationOK {
		return errors.New("安全评估记录引用原始报告，但仓库协议未提供报告同步端口")
	}
	for _, digest := range digests {
		raw, err := reportSource.ReadAssessmentReport(ctx, digest)
		if err != nil {
			return fmt.Errorf("下载安全评估报告 %s: %w", digest, err)
		}
		if err := reportDestination.PutAssessmentReport(ctx, digest, raw); err != nil {
			return fmt.Errorf("写入安全评估报告 %s: %w", digest, err)
		}
	}
	return nil
}

func assessmentReportDigests(envelope artifacttrust.Envelope) ([]string, error) {
	values := map[string]struct{}{}
	collect := func(evaluation artifactassessment.Evaluation) {
		for _, digest := range []string{evaluation.Vulnerabilities.ReportSHA256, evaluation.Licenses.ReportSHA256} {
			if digest != "" {
				values[digest] = struct{}{}
			}
		}
	}
	if len(envelope.SecurityAdmission) != 0 {
		record, _, err := artifactassessment.InspectAdmission(envelope.SecurityAdmission)
		if err != nil {
			return nil, err
		}
		collect(record.Evaluation)
	}
	statusRecords, err := artifactassessment.InspectStatusChain(envelope.SecurityStatusChain)
	if err != nil {
		return nil, err
	}
	for _, raw := range statusRecords {
		record, _, err := artifactassessment.InspectStatus(raw)
		if err != nil {
			return nil, err
		}
		collect(record.Evaluation)
	}
	digests := make([]string, 0, len(values))
	for digest := range values {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return digests, nil
}
