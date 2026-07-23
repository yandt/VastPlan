package artifactprovenance

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type dsseEnvelope struct {
	PayloadType string          `json:"payloadType"`
	Payload     string          `json:"payload"`
	Signatures  []dsseSignature `json:"signatures"`
}

type dsseSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

type inTotoStatement struct {
	Type          string          `json:"_type"`
	Subject       []subject       `json:"subject"`
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

type subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type slsaPredicate struct {
	BuildDefinition struct {
		BuildType            string               `json:"buildType"`
		ResolvedDependencies []resolvedDependency `json:"resolvedDependencies"`
	} `json:"buildDefinition"`
	RunDetails struct {
		Builder struct {
			ID string `json:"id"`
		} `json:"builder"`
	} `json:"runDetails"`
}

type resolvedDependency struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

func InspectDSSE(raw []byte, expectedSubjectSHA256 string) (StatementSummary, string, error) {
	if len(raw) == 0 || len(raw) > MaxProvenanceBytes {
		return StatementSummary{}, "", fmt.Errorf("来源证明大小必须为 1-%d 字节", MaxProvenanceBytes)
	}
	if !validSHA256(expectedSubjectSHA256) {
		return StatementSummary{}, "", errors.New("来源证明期望 subject 不是规范 SHA-256")
	}
	envelope, payload, err := decodeEnvelope(raw)
	if err != nil {
		return StatementSummary{}, "", err
	}
	if len(envelope.Signatures) == 0 || len(envelope.Signatures) > 16 {
		return StatementSummary{}, "", errors.New("DSSE signatures 数量必须为 1-16")
	}
	var statement inTotoStatement
	if err := decodeStrict(payload, &statement); err != nil {
		return StatementSummary{}, "", fmt.Errorf("解析 in-toto Statement: %w", err)
	}
	if statement.Type != InTotoStatementType || statement.PredicateType != SLSAProvenanceType {
		return StatementSummary{}, "", errors.New("来源证明必须是 in-toto Statement v1 + SLSA Provenance v1")
	}
	if len(statement.Subject) == 0 || len(statement.Subject) > 64 {
		return StatementSummary{}, "", errors.New("in-toto subject 数量必须为 1-64")
	}
	matched := false
	for _, item := range statement.Subject {
		if len(item.Name) == 0 || len(item.Name) > 512 || len(item.Digest) == 0 || len(item.Digest) > 16 {
			return StatementSummary{}, "", errors.New("in-toto subject 无效")
		}
		if strings.EqualFold(item.Digest["sha256"], expectedSubjectSHA256) {
			matched = true
		}
	}
	if !matched {
		return StatementSummary{}, "", errors.New("in-toto subject 未绑定最终插件包 SHA-256")
	}
	var predicate slsaPredicate
	if err := json.Unmarshal(statement.Predicate, &predicate); err != nil {
		return StatementSummary{}, "", fmt.Errorf("解析 SLSA Provenance v1: %w", err)
	}
	summary := StatementSummary{PredicateType: statement.PredicateType, BuilderID: strings.TrimSpace(predicate.RunDetails.Builder.ID), BuildType: strings.TrimSpace(predicate.BuildDefinition.BuildType)}
	if summary.BuilderID == "" || len(summary.BuilderID) > 2048 || summary.BuildType == "" || len(summary.BuildType) > 2048 {
		return StatementSummary{}, "", errors.New("SLSA builder.id/buildType 不能为空或超限")
	}
	if len(predicate.BuildDefinition.ResolvedDependencies) > 1024 {
		return StatementSummary{}, "", errors.New("SLSA resolvedDependencies 超过 1024")
	}
	for _, dependency := range predicate.BuildDefinition.ResolvedDependencies {
		source, err := normalizeSource(dependency.URI, dependency.Digest)
		if err != nil {
			return StatementSummary{}, "", err
		}
		summary.Sources = append(summary.Sources, source)
	}
	sortSources(summary.Sources)
	digest := sha256.Sum256(raw)
	return summary, hex.EncodeToString(digest[:]), nil
}

func VerifyDSSEEd25519(raw []byte, expectedSubjectSHA256 string, keys map[string]ed25519.PublicKey) (StatementSummary, string, error) {
	summary, digest, err := InspectDSSE(raw, expectedSubjectSHA256)
	if err != nil {
		return StatementSummary{}, "", err
	}
	envelope, payload, err := decodeEnvelope(raw)
	if err != nil {
		return StatementSummary{}, "", err
	}
	message := pae(envelope.PayloadType, payload)
	for _, signature := range envelope.Signatures {
		publicKey := keys[strings.TrimSpace(signature.KeyID)]
		decoded, decodeErr := base64.StdEncoding.DecodeString(signature.Sig)
		if len(publicKey) == ed25519.PublicKeySize && decodeErr == nil && len(decoded) == ed25519.SignatureSize && ed25519.Verify(publicKey, message, decoded) {
			return summary, digest, nil
		}
	}
	return StatementSummary{}, "", errors.New("DSSE 没有受信任的有效 Ed25519 签名")
}

func decodeEnvelope(raw []byte) (dsseEnvelope, []byte, error) {
	var envelope dsseEnvelope
	if err := decodeStrict(raw, &envelope); err != nil {
		return dsseEnvelope{}, nil, fmt.Errorf("解析 DSSE envelope: %w", err)
	}
	if envelope.PayloadType != DSSEPayloadType || len(envelope.Payload) == 0 {
		return dsseEnvelope{}, nil, errors.New("DSSE payloadType/payload 无效")
	}
	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil || len(payload) == 0 || len(payload) > MaxProvenanceBytes {
		return dsseEnvelope{}, nil, errors.New("DSSE payload 不是有界标准 base64")
	}
	for _, signature := range envelope.Signatures {
		if signature.KeyID == "" || len(signature.KeyID) > 256 || signature.Sig == "" {
			return dsseEnvelope{}, nil, errors.New("DSSE signature keyid/sig 无效")
		}
	}
	return envelope, payload, nil
}

func pae(payloadType string, payload []byte) []byte {
	return bytes.Join([][]byte{[]byte("DSSEv1"), []byte(strconv.Itoa(len(payloadType))), []byte(payloadType), []byte(strconv.Itoa(len(payload))), payload}, []byte(" "))
}

func normalizeSource(uri string, values map[string]string) (Source, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" || len(uri) > 4096 || len(values) > 16 {
		return Source{}, errors.New("SLSA resolved dependency URI/digest 无效")
	}
	result := Source{URI: uri}
	for algorithm, value := range values {
		algorithm, value = strings.ToLower(strings.TrimSpace(algorithm)), strings.ToLower(strings.TrimSpace(value))
		if algorithm == "" || len(algorithm) > 64 || value == "" || len(value) > 256 {
			return Source{}, errors.New("SLSA resolved dependency digest 无效")
		}
		result.Digests = append(result.Digests, Digest{Algorithm: algorithm, Value: value})
	}
	sort.Slice(result.Digests, func(i, j int) bool {
		if result.Digests[i].Algorithm != result.Digests[j].Algorithm {
			return result.Digests[i].Algorithm < result.Digests[j].Algorithm
		}
		return result.Digests[i].Value < result.Digests[j].Value
	})
	return result, nil
}

func sortSources(values []Source) {
	sort.Slice(values, func(i, j int) bool {
		left, _ := json.Marshal(values[i].Digests)
		right, _ := json.Marshal(values[j].Digests)
		if values[i].URI != values[j].URI {
			return values[i].URI < values[j].URI
		}
		return bytes.Compare(left, right) < 0
	})
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("JSON 包含多个值")
	}
	return nil
}
