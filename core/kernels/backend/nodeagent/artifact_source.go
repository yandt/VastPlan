package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

// ArtifactSource 只负责获取未信任 Envelope。它可以由内置 file/HTTPS 实现或未来
// 预置基础插件适配，但不能声明制品已经可信。
type ArtifactSource interface {
	Fetch(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error)
}

// ArtifactProofVerifier 是内核组合根注入的发布者证明验证器，不属于插件 SPI。
// 普通 ArtifactSource 无法替换它，也不能构造 VerifiedArtifact。
type ArtifactProofVerifier interface {
	VerifyProof(artifacttrust.Envelope) error
}

// ArtifactVerifier 是内核固定强制点。字段不导出，生产代码只能通过安全构造器
// 选择“必须有可信证明”或显式的本地开发无签名模式。
type ArtifactVerifier struct {
	proofVerifier ArtifactProofVerifier
	allowUnsigned bool
	configured    bool
	validate      func(pluginv1.Artifact, []byte) error
}

func NewSignedArtifactVerifier(proofVerifier ArtifactProofVerifier) (ArtifactVerifier, error) {
	if proofVerifier == nil {
		return ArtifactVerifier{}, errors.New("签名制品验证器必须配置发布者证明验证器")
	}
	return ArtifactVerifier{proofVerifier: proofVerifier, configured: true, validate: artifacttrust.ValidateContent}, nil
}

// NewLocalDevelopmentArtifactVerifier 仅供显式本地文件仓库使用。它仍强制元数据、
// SHA-256、大小、根清单和法律文件绑定，只省略发布者证明。
func NewLocalDevelopmentArtifactVerifier() ArtifactVerifier {
	return ArtifactVerifier{allowUnsigned: true, configured: true, validate: artifacttrust.ValidateContent}
}

// NewLocalDevelopmentArtifactVerifierWithTrust is used only by an explicitly
// development-mode host that consumes both unsigned local Seed artifacts and
// signed testing artifacts. Signed envelopes are still verified; this does not
// weaken the production constructor, which continues to require every artifact
// to carry a trusted proof.
func NewLocalDevelopmentArtifactVerifierWithTrust(proofVerifier ArtifactProofVerifier) (ArtifactVerifier, error) {
	if proofVerifier == nil {
		return ArtifactVerifier{}, errors.New("开发混合制品验证器必须配置发布者信任")
	}
	return ArtifactVerifier{proofVerifier: proofVerifier, allowUnsigned: true, configured: true, validate: artifacttrust.ValidateContent}, nil
}

// VerifiedArtifact 只能由 ArtifactVerifier 构造。安装器接收该类型而非来源直接返回的
// Artifact+bytes，避免可插拔制品源绕过内核验证链。
type VerifiedArtifact struct {
	artifact               pluginv1.Artifact
	packageBytes           []byte
	proof                  []byte
	provenance             []byte
	provenanceVerification []byte
	securityAdmission      []byte
	securityStatusChain    []byte
	verified               bool
}

func (v VerifiedArtifact) Artifact() pluginv1.Artifact { return v.artifact }

func (v VerifiedArtifact) PackageBytes() []byte { return append([]byte(nil), v.packageBytes...) }

// ProofBytes returns the already verified publisher proof for trusted host
// adapters such as Bootstrap upgrade mirroring. Plugins never receive it.
func (v VerifiedArtifact) ProofBytes() []byte { return append([]byte(nil), v.proof...) }

func (v VerifiedArtifact) ProvenanceBytes() []byte {
	return append([]byte(nil), v.provenance...)
}

func (v VerifiedArtifact) ProvenanceVerificationBytes() []byte {
	return append([]byte(nil), v.provenanceVerification...)
}

func (v VerifiedArtifact) SecurityAdmissionBytes() []byte {
	return append([]byte(nil), v.securityAdmission...)
}

func (v VerifiedArtifact) SecurityStatusChainBytes() []byte {
	return append([]byte(nil), v.securityStatusChain...)
}

func (v ArtifactVerifier) Verify(ref pluginv1.ArtifactRef, envelope artifacttrust.Envelope) (VerifiedArtifact, error) {
	if !v.configured || v.validate == nil {
		return VerifiedArtifact{}, errors.New("内核制品验证器未配置")
	}
	artifact := envelope.Artifact
	if artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.Channel != ref.Channel {
		return VerifiedArtifact{}, errors.New("制品 Envelope 与请求引用不一致")
	}
	if err := v.validate(artifact, envelope.PackageBytes); err != nil {
		return VerifiedArtifact{}, fmt.Errorf("制品内容验证失败: %w", err)
	}
	if len(bytes.TrimSpace(envelope.Proof)) == 0 {
		if len(bytes.TrimSpace(envelope.Provenance)) != 0 || len(bytes.TrimSpace(envelope.ProvenanceVerification)) != 0 || len(bytes.TrimSpace(envelope.SecurityAdmission)) != 0 || len(bytes.TrimSpace(envelope.SecurityStatusChain)) != 0 {
			return VerifiedArtifact{}, errors.New("无发布者证明的本地制品不得携带供应链 sidecar")
		}
		if !v.allowUnsigned {
			return VerifiedArtifact{}, errors.New("签名模式下制品缺少发布者证明")
		}
	} else {
		if v.proofVerifier == nil {
			return VerifiedArtifact{}, errors.New("制品包含证明但内核未配置证明验证器")
		}
		if err := v.proofVerifier.VerifyProof(envelope); err != nil {
			return VerifiedArtifact{}, fmt.Errorf("发布者证明验证失败: %w", err)
		}
	}
	return VerifiedArtifact{
		artifact: artifact, packageBytes: append([]byte(nil), envelope.PackageBytes...),
		proof: append([]byte(nil), envelope.Proof...), provenance: append([]byte(nil), envelope.Provenance...),
		provenanceVerification: append([]byte(nil), envelope.ProvenanceVerification...),
		securityAdmission:      append([]byte(nil), envelope.SecurityAdmission...),
		securityStatusChain:    append([]byte(nil), envelope.SecurityStatusChain...), verified: true,
	}, nil
}

func sourceName(source ArtifactSource) string {
	if named, ok := source.(interface{ SourceName() string }); ok && named.SourceName() != "" {
		return named.SourceName()
	}
	return fmt.Sprintf("%T", source)
}
