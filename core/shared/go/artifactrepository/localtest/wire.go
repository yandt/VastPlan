package localtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

const (
	maxMetadataBytes     = int64(2 << 20)
	maxProofBytes        = int64(2 << 20)
	maxProvenanceBytes   = int64(2 << 20)
	maxVerificationBytes = int64(256 << 10)
	maxAdmissionBytes    = int64(256 << 10)
	maxStatusBytes       = int64(2 << 20)
	maxMultipartOverhead = int64(1 << 20)
)

var envelopeFields = []struct {
	name        string
	contentType string
	value       func(artifacttrust.Envelope) []byte
}{
	{"package", "application/gzip", func(value artifacttrust.Envelope) []byte { return value.PackageBytes }},
	{"proof", "application/json", func(value artifacttrust.Envelope) []byte { return value.Proof }},
	{"provenance", "application/json", func(value artifacttrust.Envelope) []byte { return value.Provenance }},
	{"provenance-verification", "application/json", func(value artifacttrust.Envelope) []byte { return value.ProvenanceVerification }},
	{"security-admission", "application/json", func(value artifacttrust.Envelope) []byte { return value.SecurityAdmission }},
	{"security-status-chain", "application/json", func(value artifacttrust.Envelope) []byte { return value.SecurityStatusChain }},
}

func writeEnvelope(writer *multipart.Writer, envelope artifacttrust.Envelope) error {
	metadata, err := json.Marshal(envelope.Artifact)
	if err != nil {
		return err
	}
	if err := writeEnvelopePart(writer, "artifact", "application/json", metadata); err != nil {
		return err
	}
	for _, field := range envelopeFields {
		value := field.value(envelope)
		if len(value) == 0 {
			continue
		}
		if err := writeEnvelopePart(writer, field.name, field.contentType, value); err != nil {
			return err
		}
	}
	return nil
}

func writeEnvelopePart(writer *multipart.Writer, name, contentType string, value []byte) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q`, name))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(value)
	return err
}

func readEnvelope(body io.Reader, contentType string) (artifacttrust.Envelope, error) {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") || parameters["boundary"] == "" {
		return artifacttrust.Envelope{}, errors.New("local-test Envelope 必须使用 multipart/form-data")
	}
	reader := multipart.NewReader(body, parameters["boundary"])
	values := map[string][]byte{}
	limits := map[string]int64{
		"artifact": maxMetadataBytes, "package": MaxPackageBytes, "proof": maxProofBytes,
		"provenance": maxProvenanceBytes, "provenance-verification": maxVerificationBytes,
		"security-admission": maxAdmissionBytes, "security-status-chain": maxStatusBytes,
	}
	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			return artifacttrust.Envelope{}, fmt.Errorf("读取 local-test multipart: %w", partErr)
		}
		name := part.FormName()
		limit, known := limits[name]
		if !known || values[name] != nil {
			_ = part.Close()
			return artifacttrust.Envelope{}, fmt.Errorf("local-test multipart 字段未知或重复: %q", name)
		}
		value, readErr := readLimited(part, limit)
		_ = part.Close()
		if readErr != nil {
			return artifacttrust.Envelope{}, readErr
		}
		values[name] = value
	}
	if len(values["artifact"]) == 0 || len(values["package"]) == 0 || len(values["proof"]) == 0 {
		return artifacttrust.Envelope{}, errors.New("local-test Envelope 缺少 artifact、package 或 proof")
	}
	if err := pluginv1.ValidateArtifactMetadata(values["artifact"]); err != nil {
		return artifacttrust.Envelope{}, err
	}
	var artifact pluginv1.Artifact
	if err := json.Unmarshal(values["artifact"], &artifact); err != nil {
		return artifacttrust.Envelope{}, err
	}
	for _, name := range []string{"proof", "provenance", "provenance-verification", "security-admission", "security-status-chain"} {
		if value := values[name]; len(value) != 0 && !json.Valid(value) {
			return artifacttrust.Envelope{}, fmt.Errorf("local-test %s 不是有效 JSON", name)
		}
	}
	envelope := artifacttrust.Envelope{
		Artifact: artifact, PackageBytes: values["package"], Proof: values["proof"],
		Provenance: values["provenance"], ProvenanceVerification: values["provenance-verification"],
		SecurityAdmission: values["security-admission"], SecurityStatusChain: values["security-status-chain"],
	}
	if err := validateEnvelope(envelope); err != nil {
		return artifacttrust.Envelope{}, err
	}
	return envelope, nil
}

func validateEnvelope(envelope artifacttrust.Envelope) error {
	if int64(len(envelope.PackageBytes)) != envelope.Artifact.Size {
		return errors.New("local-test Envelope 制品大小不匹配")
	}
	digest := sha256.Sum256(envelope.PackageBytes)
	if hex.EncodeToString(digest[:]) != envelope.Artifact.SHA256 {
		return errors.New("local-test Envelope 制品摘要不匹配")
	}
	return nil
}

func validateEnvelopeForProfile(profile artifactrepositoryv1.Profile, envelope artifacttrust.Envelope) error {
	if err := validateEnvelope(envelope); err != nil {
		return err
	}
	metadata, err := json.Marshal(envelope.Artifact)
	if err != nil {
		return err
	}
	if err := pluginv1.ValidateArtifactMetadata(metadata); err != nil {
		return err
	}
	if len(envelope.Proof) == 0 || !json.Valid(envelope.Proof) {
		return errors.New("local-test Envelope 缺少有效发布者证明")
	}
	return artifactrepositoryv1.ValidateRef(profile, exactRef(envelope))
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > limit {
		return nil, fmt.Errorf("local-test multipart 字段超过 %d 字节", limit)
	}
	return value, nil
}

func maxRequestBytes() int64 {
	return MaxPackageBytes + maxMetadataBytes + maxProofBytes + maxProvenanceBytes + maxVerificationBytes + maxAdmissionBytes + maxStatusBytes + maxMultipartOverhead
}

func exactRef(envelope artifacttrust.Envelope) pluginv1.ArtifactRef {
	return pluginv1.ArtifactRef{PluginID: envelope.Artifact.PluginID, Version: envelope.Artifact.Version, Channel: envelope.Artifact.Channel}
}

func sameRef(left, right pluginv1.ArtifactRef) bool { return left == right }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
