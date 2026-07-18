// Package compositioncommonv1 defines the language-neutral identity, target,
// origin and resolution-reference vocabulary shared by all kernel composition
// contracts. Kernel-specific payloads deliberately live outside this package.
package compositioncommonv1

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	SchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/common/v1/vastplan.composition-common.schema.json"

	KernelBackend  = "backend"
	KernelFrontend = "frontend"
	KernelRunner   = "runner"
	KernelMobile   = "mobile"

	OriginPlatformProfile = "platform-profile"
	OriginApplication     = "application"
)

//go:embed vastplan.composition-common.schema.json
var schemaJSON []byte

// Document is the common versioned identity embedded by both authorized
// composition inputs. Payload and publisher authorization remain kernel-owned.
type Document struct {
	Version  int    `json:"version"`
	Revision uint64 `json:"revision"`
	ID       string `json:"id"`
}

type Target struct {
	Kernel string `json:"kernel"`
}

// Ref pins an input document in a kernel-specific immutable resolution output.
type Ref struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

// AddResources registers the common vocabulary for kernel-specific JSON
// Schemas. This file is a referenced resource, not a standalone input format.
func AddResources(compiler *jsonschema.Compiler) error {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return fmt.Errorf("解析组合公共 Schema: %w", err)
	}
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		return fmt.Errorf("登记组合公共 Schema: %w", err)
	}
	return nil
}

func ValidateTarget(target Target, expectedKernel string) error {
	if err := ValidateKernel(target.Kernel); err != nil {
		return err
	}
	if target.Kernel != expectedKernel {
		return fmt.Errorf("组合目标 kernel 必须为 %q，实际为 %q", expectedKernel, target.Kernel)
	}
	return nil
}

func ValidateKernel(kernel string) error {
	switch kernel {
	case KernelBackend, KernelFrontend, KernelRunner, KernelMobile:
		return nil
	default:
		return fmt.Errorf("未知组合目标 kernel %q", kernel)
	}
}

func ValidateOrigin(origin string) error {
	switch origin {
	case OriginPlatformProfile, OriginApplication:
		return nil
	default:
		return fmt.Errorf("未知组合来源 %q", origin)
	}
}

// Digest computes the canonical digest used by every kernel resolution lock.
// Callers must pass schema-validated typed values so normalization happens
// before hashing.
func Digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
