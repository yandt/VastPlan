package catalog

import (
	"encoding/json"
	"errors"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactprovenance"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactsupplychain"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pythonlock"
)

func (s *Store) Evidence(ref pluginv1.ArtifactRef) (SupplyChainEvidence, error) {
	s.mu.RLock()
	entry, ok := s.entries[refKey(ref)]
	s.mu.RUnlock()
	if !ok {
		return SupplyChainEvidence{}, errors.New("制品不存在")
	}
	_, proof, err := s.repository.ReadMetadataWithAttestation(ref)
	if err != nil {
		return SupplyChainEvidence{}, errors.New("制品供应链证明复验失败")
	}
	page := s.Publications()
	related := make([]Publication, 0)
	for _, item := range page.Items {
		if item.Source == ref || item.Target == ref {
			related = append(related, item)
		}
	}
	evidence := SupplyChainEvidence{Ref: ref, SHA256: entry.SHA256, Size: entry.Size, Publisher: entry.Publisher, KeyID: entry.KeyID, SignedAt: entry.SignedAt.Format(time.RFC3339Nano), AttestationSHA256: digestBytes(proof), Verification: "verified", Name: entry.Name, Description: entry.Description, License: entry.License, Targets: append([]string(nil), entry.Targets...), Engines: cloneStringMap(entry.Engines), RepositoryRevision: entry.RepositoryRevision, LifecycleStatus: entry.LifecycleStatus, Publications: related}
	var packageBytes []byte
	var packageManifest pluginv1.Manifest
	if entry.SBOM != nil || entry.PythonLock != nil {
		reader, ok := s.repository.(verifiedPackageReader)
		if !ok {
			return SupplyChainEvidence{}, errors.New("仓库不支持复验供应链包体")
		}
		artifact, currentPackage, currentProof, err := reader.ReadWithAttestation(ref)
		if err != nil || artifact.SHA256 != entry.SHA256 || digestBytes(currentProof) != evidence.AttestationSHA256 {
			return SupplyChainEvidence{}, errors.New("制品供应链包体或证明复验失败")
		}
		packageManifest, err = pluginv1.ParseManifest(artifact.Manifest)
		if err != nil || packageManifest.SupplyChain == nil {
			return SupplyChainEvidence{}, errors.New("制品供应链签名声明缺失")
		}
		packageBytes = currentPackage
	}
	if entry.SBOM != nil {
		if packageManifest.SupplyChain.SBOM == nil {
			return SupplyChainEvidence{}, errors.New("制品 SBOM 签名声明缺失")
		}
		raw, err := artifacttrust.ReadPackageFile(packageBytes, packageManifest.SupplyChain.SBOM.Path, artifactsupplychain.MaxCycloneDXBytes)
		if err != nil {
			return SupplyChainEvidence{}, errors.New("读取制品 SBOM 失败")
		}
		summary, err := artifactsupplychain.InspectCycloneDX(raw)
		if err != nil || summary.SHA256 != entry.SBOM.SHA256 || summary.RootName != ref.PluginID || summary.RootVersion != ref.Version {
			return SupplyChainEvidence{}, errors.New("制品 SBOM 摘要或主体复验失败")
		}
		evidence.SBOM = &platformadminapi.ArtifactSBOMEvidence{ArtifactSBOMDeclaration: *entry.SBOM, SerialNumber: summary.SerialNumber, Components: summary.Components, Verification: "verified"}
	}
	if entry.PythonLock != nil {
		if packageManifest.SupplyChain.PythonLock == nil {
			return SupplyChainEvidence{}, errors.New("制品 Python 锁签名声明缺失")
		}
		raw, err := artifacttrust.ReadPackageFile(packageBytes, packageManifest.SupplyChain.PythonLock.Path, pythonlock.MaxLockBytes)
		if err != nil {
			return SupplyChainEvidence{}, errors.New("读取制品 Python 锁失败")
		}
		summary, err := pythonlock.Inspect(raw)
		if err != nil || summary.SHA256 != entry.PythonLock.SHA256 {
			return SupplyChainEvidence{}, errors.New("制品 Python 锁摘要或内容复验失败")
		}
		evidence.PythonLock = &platformadminapi.ArtifactPythonLockEvidence{ArtifactPythonLockDeclaration: *entry.PythonLock, RequiresPython: summary.RequiresPython, CreatedBy: summary.CreatedBy, Packages: len(summary.Packages), Wheels: len(summary.Wheels), Verification: "verified"}
	}
	if entry.Provenance != nil {
		reader, ok := s.repository.(verifiedProvenanceReader)
		if !ok {
			return SupplyChainEvidence{}, errors.New("仓库不支持复验来源证明 sidecar")
		}
		provenanceRaw, verificationRaw, err := reader.ReadProvenance(ref)
		if err != nil || digestBytes(provenanceRaw) != entry.Provenance.ProvenanceSHA256 || digestBytes(verificationRaw) != entry.Provenance.VerificationSHA256 {
			return SupplyChainEvidence{}, errors.New("来源证明 sidecar 复验失败")
		}
		summary, _, err := artifactprovenance.InspectDSSE(provenanceRaw, entry.SHA256)
		record, _, recordErr := artifactprovenance.InspectVerificationRecord(verificationRaw)
		if err != nil || recordErr != nil || record.SubjectSHA256 != entry.SHA256 || record.ProvenanceSHA256 != entry.Provenance.ProvenanceSHA256 || !sameProvenanceSummary(record.StatementSummary, summary) {
			return SupplyChainEvidence{}, errors.New("来源证明内容复验失败")
		}
		evidence.Provenance = &platformadminapi.ArtifactProvenanceEvidence{ArtifactProvenanceDeclaration: *entry.Provenance, Sources: len(record.Sources), Verification: "verified"}
	}
	if entry.SecurityAdmission != nil {
		reader, ok := s.repository.(verifiedSecurityAdmissionReader)
		if !ok {
			return SupplyChainEvidence{}, errors.New("仓库不支持复验安全准入记录")
		}
		raw, err := reader.ReadSecurityAdmission(ref)
		if err != nil || digestBytes(raw) != entry.SecurityAdmission.AdmissionSHA256 {
			return SupplyChainEvidence{}, errors.New("安全准入记录 sidecar 复验失败")
		}
		record, _, err := artifactassessment.InspectAdmission(raw)
		if err != nil || record.Evaluation.SubjectSHA256 != entry.SHA256 || entry.SBOM == nil || record.Evaluation.SBOMSHA256 != entry.SBOM.SHA256 {
			return SupplyChainEvidence{}, errors.New("安全准入记录内容复验失败")
		}
		evidence.SecurityAdmission = &platformadminapi.ArtifactSecurityAdmissionEvidence{
			ArtifactSecurityAdmissionDeclaration: *entry.SecurityAdmission,
			VulnerabilityReportSHA256:            record.Evaluation.Vulnerabilities.ReportSHA256,
			LicenseReportSHA256:                  record.Evaluation.Licenses.ReportSHA256,
			Verification:                         "verified",
		}
	}
	if reader, ok := s.repository.(verifiedSecurityStatusReader); ok {
		chainRaw, err := reader.ReadSecurityStatusChain(ref)
		if err != nil {
			return SupplyChainEvidence{}, errors.New("安全复扫状态链读取失败")
		}
		records, err := artifactassessment.InspectStatusChain(chainRaw)
		if err != nil {
			return SupplyChainEvidence{}, errors.New("安全复扫状态链解析失败")
		}
		if len(records) > 0 {
			latest, digest, err := artifactassessment.InspectStatus(records[len(records)-1])
			if err != nil {
				return SupplyChainEvidence{}, errors.New("最新安全复扫状态无效")
			}
			evidence.SecurityStatus = &platformadminapi.ArtifactSecurityStatusEvidence{
				Sequence: latest.Sequence, RecordSHA256: digest, PreviousSHA256: latest.PreviousSHA256,
				Decision: latest.Evaluation.Decision, DatabaseRevision: latest.Evaluation.Scanner.DatabaseRevision,
				EvaluatedAt: latest.Evaluation.EvaluatedAt.Format(time.RFC3339Nano), ExpiresAt: latest.Evaluation.ExpiresAt.Format(time.RFC3339Nano),
				Critical: latest.Evaluation.Vulnerabilities.Critical, High: latest.Evaluation.Vulnerabilities.High,
				DeniedLicense: latest.Evaluation.Licenses.Denied, UnknownLicense: latest.Evaluation.Licenses.Unknown,
				VulnerabilityReportSHA256: latest.Evaluation.Vulnerabilities.ReportSHA256,
				LicenseReportSHA256:       latest.Evaluation.Licenses.ReportSHA256,
				Verification:              "verified",
			}
		}
	}
	return evidence, nil
}

func sameProvenanceSummary(left, right artifactprovenance.StatementSummary) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}
