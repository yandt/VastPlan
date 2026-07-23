// Command artifactassessment scans one final plugin package and emits a signed
// AdmissionRecord plus the exact external report bound by its digest.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactsupplychain"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	options, err := parseOptions()
	if err != nil {
		return err
	}
	if options.printDatabaseRevision {
		revision, err := provider.TrivyDatabaseRevision(options.cacheDirectory)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, revision)
		return nil
	}
	maximum, err := options.maximum()
	if err != nil {
		return err
	}
	packageBytes, err := readRegular(options.packagePath, maxPackageBytes, false)
	if err != nil {
		return err
	}
	manifest, _, err := artifacttrust.InspectPackage(packageBytes)
	if err != nil {
		return err
	}
	if manifest.SupplyChain == nil || manifest.SupplyChain.SBOM == nil {
		return fmt.Errorf("插件 %s 没有签名清单绑定的 CycloneDX SBOM", manifest.ID)
	}
	sbom, err := artifacttrust.ReadPackageFile(packageBytes, manifest.SupplyChain.SBOM.Path, artifactsupplychain.MaxCycloneDXBytes)
	if err != nil {
		return err
	}
	privateKey, err := readPrivateKey(options.privateKeyPath)
	if err != nil {
		return err
	}
	engine, err := provider.NewTrivy(provider.TrivyConfig{
		Binary: options.trivyPath, CacheDirectory: options.cacheDirectory, ScannerVersion: options.scannerVersion,
		DatabaseRevision: options.databaseRevision, Timeout: options.timeout, AllowedLicenses: options.licenses(), FullLicenseScan: options.fullLicenseScan,
	})
	if err != nil {
		return err
	}
	service, err := provider.New(provider.Config{ProviderID: options.providerID, KeyID: options.keyID, TTL: options.ttl, Maximum: maximum}, privateKey, engine, options.workRoot)
	if err != nil {
		return err
	}
	packageDigest, sbomDigest := sha256.Sum256(packageBytes), sha256.Sum256(sbom)
	evidence, err := service.AssessWithEvidence(ctx, artifactassessment.ScanRequest{
		Identity: artifactassessment.ArtifactIdentity{PluginID: manifest.ID, Channel: options.channel, Publisher: manifest.Publisher, SHA256: hex.EncodeToString(packageDigest[:]), SBOMSHA256: hex.EncodeToString(sbomDigest[:])},
		Package:  packageBytes, SBOM: sbom, PolicyID: options.policyID,
	})
	if err != nil {
		return err
	}
	record, err := json.MarshalIndent(evidence.Admission, "", "  ")
	if err != nil {
		return err
	}
	record = append(record, '\n')
	if err := writeAtomic(options.reportPath, evidence.Report); err != nil {
		return fmt.Errorf("写入扫描报告: %w", err)
	}
	if err := writeAtomic(options.outputPath, record); err != nil {
		return fmt.Errorf("写入准入记录: %w", err)
	}
	fmt.Fprintf(os.Stdout, "assessment=%s decision=%s reportSha256=%s\n", options.outputPath, evidence.Admission.Evaluation.Decision, evidence.Admission.Evaluation.Vulnerabilities.ReportSHA256)
	if evidence.Admission.Evaluation.Decision != artifactassessment.DecisionPass {
		return errorsNewDecisionFailed()
	}
	return nil
}

func errorsNewDecisionFailed() error { return fmt.Errorf("安全评估未通过策略阈值") }
