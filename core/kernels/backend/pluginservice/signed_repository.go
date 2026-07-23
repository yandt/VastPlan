package pluginservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

// SignedRepository 在本地不可变仓库上增加发布者签名强制点。
type SignedRepository struct {
	Local *Repository
	Trust *TrustStore
	mu    sync.Mutex // 证明写入与不可变性检查必须在同一临界区。
}

func (r *SignedRepository) SourceName() string { return "bootstrap-signed-file" }

func (r *SignedRepository) Publish(attestation Attestation, packageBytes []byte) (Artifact, error) {
	return r.PublishWithProvenance(attestation, packageBytes, nil, nil)
}

func (r *SignedRepository) PublishWithProvenance(attestation Attestation, packageBytes, provenanceRaw, verificationRaw []byte) (Artifact, error) {
	return r.PublishWithSupplyChain(attestation, packageBytes, provenanceRaw, verificationRaw, nil)
}

func (r *SignedRepository) PublishWithSupplyChain(attestation Attestation, packageBytes, provenanceRaw, verificationRaw, admissionRaw []byte) (Artifact, error) {
	if r == nil || r.Local == nil || r.Trust == nil {
		return Artifact{}, errors.New("签名制品仓库未完整配置")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ValidateArtifact(attestation.Artifact, packageBytes); err != nil {
		return Artifact{}, err
	}
	raw, err := json.Marshal(attestation)
	if err != nil {
		return Artifact{}, err
	}
	if err := r.Trust.VerifyProof(artifacttrust.Envelope{Artifact: attestation.Artifact, PackageBytes: packageBytes, Proof: raw, Provenance: provenanceRaw, ProvenanceVerification: verificationRaw, SecurityAdmission: admissionRaw}); err != nil {
		return Artifact{}, err
	}
	r.Local.mu.Lock()
	published, err := r.Local.publishArtifact(attestation.Artifact, packageBytes)
	r.Local.mu.Unlock()
	if err != nil {
		return Artifact{}, err
	}
	dir, err := r.Local.artifactDir(Ref{PluginID: published.PluginID, Version: published.Version, Channel: published.Channel})
	if err != nil {
		return Artifact{}, err
	}
	for _, sidecar := range []struct {
		name  string
		raw   []byte
		label string
	}{
		{name: "attestation.json", raw: raw, label: "签名证明"},
		{name: "provenance.dsse.json", raw: provenanceRaw, label: "来源证明"},
		{name: "provenance-verification.json", raw: verificationRaw, label: "来源证明验证记录"},
		{name: "security-admission.json", raw: admissionRaw, label: "安全准入记录"},
	} {
		if err := writeImmutableSidecar(filepath.Join(dir, sidecar.name), sidecar.raw, sidecar.label); err != nil {
			return Artifact{}, err
		}
	}
	return published, nil
}

func (r *SignedRepository) Read(ref Ref) (Artifact, []byte, error) {
	artifact, packageBytes, _, err := r.ReadWithAttestation(ref)
	return artifact, packageBytes, err
}

// ListRefs 只枚举本地不可变索引中的精确引用。返回值尚未代表可信制品；Catalog
// 等调用方必须继续使用 ReadWithAttestation 逐项完成签名与内容复验。
func (r *SignedRepository) ListRefs() ([]Ref, error) {
	if r == nil || r.Local == nil || r.Trust == nil {
		return nil, errors.New("签名制品仓库未完整配置")
	}
	return r.Local.ListRefs()
}

// ReadMetadataWithAttestation 校验元数据与全部证明，不读取包体。它供 Catalog
// 启动重建使用，避免仓库启动时间按全部对象字节数增长；任何实际交付仍必须走
// ReadWithAttestation，对包体重新计算摘要并检查清单绑定。
func (r *SignedRepository) ReadMetadataWithAttestation(ref Ref) (Artifact, []byte, error) {
	artifact, proof, _, _, _, err := r.ReadMetadataWithSupplyChain(ref)
	return artifact, proof, err
}

// ReadMetadataWithProvenance 在一次磁盘读取和信任校验中返回 Catalog 所需的全部
// 元数据 sidecar，避免调用方随后再次读取并重复验签。
func (r *SignedRepository) ReadMetadataWithProvenance(ref Ref) (Artifact, []byte, []byte, []byte, error) {
	artifact, proof, provenance, verification, _, err := r.ReadMetadataWithSupplyChain(ref)
	return artifact, proof, provenance, verification, err
}

func (r *SignedRepository) ReadMetadataWithSupplyChain(ref Ref) (Artifact, []byte, []byte, []byte, []byte, error) {
	if r == nil || r.Local == nil || r.Trust == nil {
		return Artifact{}, nil, nil, nil, nil, errors.New("签名制品仓库未完整配置")
	}
	artifact, err := r.Local.ReadMetadata(ref)
	if err != nil {
		return Artifact{}, nil, nil, nil, nil, err
	}
	dir, err := r.Local.artifactDir(ref)
	if err != nil {
		return Artifact{}, nil, nil, nil, nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "attestation.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Artifact{}, nil, nil, nil, nil, errors.New("签名制品缺少发布者证明")
		}
		return Artifact{}, nil, nil, nil, nil, fmt.Errorf("读取签名证明: %w", err)
	}
	var attestation Attestation
	if err := decodeJSONStrict(raw, &attestation); err != nil {
		return Artifact{}, nil, nil, nil, nil, fmt.Errorf("解析签名证明: %w", err)
	}
	if !sameArtifact(artifact, attestation.Artifact) {
		return Artifact{}, nil, nil, nil, nil, errors.New("签名证明与制品元数据不一致")
	}
	provenanceRaw, verificationRaw, err := readProvenanceSidecars(dir)
	if err != nil {
		return Artifact{}, nil, nil, nil, nil, err
	}
	admissionRaw, err := readSecurityAdmissionSidecar(dir)
	if err != nil {
		return Artifact{}, nil, nil, nil, nil, err
	}
	if err := r.Trust.VerifyProof(artifacttrust.Envelope{Artifact: artifact, Proof: raw, Provenance: provenanceRaw, ProvenanceVerification: verificationRaw, SecurityAdmission: admissionRaw}); err != nil {
		return Artifact{}, nil, nil, nil, nil, err
	}
	return artifact, append([]byte(nil), raw...), append([]byte(nil), provenanceRaw...), append([]byte(nil), verificationRaw...), append([]byte(nil), admissionRaw...), nil
}

// ReadWithAttestation 返回通过内核信任根验证的制品、原始包和签名证明。
// 对外 HTTP 层只能转发这份已验证的结果，不能自行读取仓库目录或绕过验签。
func (r *SignedRepository) ReadWithAttestation(ref Ref) (Artifact, []byte, []byte, error) {
	if r == nil || r.Local == nil || r.Trust == nil {
		return Artifact{}, nil, nil, errors.New("签名制品仓库未完整配置")
	}
	envelope, err := r.Fetch(context.Background(), ref)
	if err != nil {
		return Artifact{}, nil, nil, err
	}
	if err := r.Trust.VerifyProof(envelope); err != nil {
		return Artifact{}, nil, nil, err
	}
	return envelope.Artifact, envelope.PackageBytes, append([]byte(nil), envelope.Proof...), nil
}

func (r *SignedRepository) ReadWithProvenance(ref Ref) (Artifact, []byte, []byte, []byte, []byte, error) {
	artifact, packageBytes, proof, provenance, verification, _, err := r.ReadWithSupplyChain(ref)
	return artifact, packageBytes, proof, provenance, verification, err
}

func (r *SignedRepository) ReadWithSupplyChain(ref Ref) (Artifact, []byte, []byte, []byte, []byte, []byte, error) {
	envelope, err := r.Fetch(context.Background(), ref)
	if err != nil {
		return Artifact{}, nil, nil, nil, nil, nil, err
	}
	if err := r.Trust.VerifyProof(envelope); err != nil {
		return Artifact{}, nil, nil, nil, nil, nil, err
	}
	return envelope.Artifact, envelope.PackageBytes, append([]byte(nil), envelope.Proof...), append([]byte(nil), envelope.Provenance...), append([]byte(nil), envelope.ProvenanceVerification...), append([]byte(nil), envelope.SecurityAdmission...), nil
}

func (r *SignedRepository) ReadProvenance(ref Ref) ([]byte, []byte, error) {
	_, _, provenanceRaw, verificationRaw, err := r.ReadMetadataWithProvenance(ref)
	return provenanceRaw, verificationRaw, err
}

func (r *SignedRepository) ReadSecurityAdmission(ref Ref) ([]byte, error) {
	_, _, _, _, admissionRaw, err := r.ReadMetadataWithSupplyChain(ref)
	return admissionRaw, err
}

// HTTPRepositoryAdapter 是 HTTP 传输层使用的窄适配器。它把不可信网络字节
// 交给内核的签名与内容强制点处理，而不向 HTTP 层暴露信任根或磁盘布局。
type HTTPRepositoryAdapter struct{ Repository *SignedRepository }

func (a HTTPRepositoryAdapter) Publish(attestationRaw, packageBytes []byte) (Artifact, error) {
	if a.Repository == nil {
		return Artifact{}, errors.New("签名制品仓库未完整配置")
	}
	var attestation Attestation
	if err := decodeJSONStrict(attestationRaw, &attestation); err != nil {
		return Artifact{}, fmt.Errorf("解析制品证明: %w", err)
	}
	return a.Repository.Publish(attestation, packageBytes)
}

func (a HTTPRepositoryAdapter) Read(ref Ref) (Artifact, []byte, []byte, error) {
	if a.Repository == nil {
		return Artifact{}, nil, nil, errors.New("签名制品仓库未完整配置")
	}
	return a.Repository.ReadWithAttestation(ref)
}

func (a HTTPRepositoryAdapter) PublishWithProvenance(attestationRaw, packageBytes, provenanceRaw, verificationRaw []byte) (Artifact, error) {
	return a.PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, nil)
}

func (a HTTPRepositoryAdapter) PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, admissionRaw []byte) (Artifact, error) {
	if a.Repository == nil {
		return Artifact{}, errors.New("签名制品仓库未完整配置")
	}
	var attestation Attestation
	if err := decodeJSONStrict(attestationRaw, &attestation); err != nil {
		return Artifact{}, fmt.Errorf("解析制品证明: %w", err)
	}
	return a.Repository.PublishWithSupplyChain(attestation, packageBytes, provenanceRaw, verificationRaw, admissionRaw)
}

func (a HTTPRepositoryAdapter) ReadWithProvenance(ref Ref) (Artifact, []byte, []byte, []byte, []byte, error) {
	if a.Repository == nil {
		return Artifact{}, nil, nil, nil, nil, errors.New("签名制品仓库未完整配置")
	}
	return a.Repository.ReadWithProvenance(ref)
}

func (a HTTPRepositoryAdapter) ReadWithSupplyChain(ref Ref) (Artifact, []byte, []byte, []byte, []byte, []byte, error) {
	if a.Repository == nil {
		return Artifact{}, nil, nil, nil, nil, nil, errors.New("签名制品仓库未完整配置")
	}
	return a.Repository.ReadWithSupplyChain(ref)
}

// Fetch 返回带原始证明的未信任 Envelope，供 Node Agent 在自己的强制点复验。
// SignedRepository.Read 的服务端预验证只是纵深防御。
func (r *SignedRepository) Fetch(_ context.Context, ref Ref) (artifacttrust.Envelope, error) {
	if r == nil || r.Local == nil || r.Trust == nil {
		return artifacttrust.Envelope{}, errors.New("签名制品仓库未完整配置")
	}
	artifact, packageBytes, err := r.Local.Read(ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifacttrust.Envelope{}, fmt.Errorf("%w: %s@%s/%s", artifacttrust.ErrNotFound, ref.PluginID, ref.Version, ref.Channel)
		}
		return artifacttrust.Envelope{}, err
	}
	dir, err := r.Local.artifactDir(ref)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "attestation.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifacttrust.Envelope{}, errors.New("签名制品缺少发布者证明")
		}
		return artifacttrust.Envelope{}, fmt.Errorf("读取签名证明: %w", err)
	}
	provenanceRaw, verificationRaw, err := readProvenanceSidecars(dir)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	admissionRaw, err := readSecurityAdmissionSidecar(dir)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	return artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: raw, Provenance: provenanceRaw, ProvenanceVerification: verificationRaw, SecurityAdmission: admissionRaw}, nil
}
