package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

type boundaryLiteralTruth struct {
	value  string
	source string
}

// Kernel implementation may authorize a concrete plugin only through a
// neutral contract identity. A raw first-party ID silently turns the kernel
// into a second source of plugin ownership truth.
func TestArch_KernelPluginIDsComeFromContracts(t *testing.T) {
	for _, file := range collectGoFiles(t) {
		if !strings.HasPrefix(file.relPath, "core/kernels/") || strings.HasSuffix(file.relPath, "_test.go") || file.generated {
			continue
		}
		for _, literal := range goStringLiterals(t, file.relPath) {
			if strings.HasPrefix(literal, "cn.vastplan.") {
				t.Errorf("内核具体插件 ID 被散写: %s 包含 %q\n  原因: core/kernels 只能引用 contracts 或中立 Library 的身份常量", file.relPath, literal)
			}
		}
	}
}

// Trusted boundary values must compile against the same source on both the
// caller and verifier sides. This guards the high-impact identities currently
// crossing kernel/plugin or host/provider boundaries.
func TestArch_TrustedBoundaryLiteralsHaveSingleSource(t *testing.T) {
	truths := []boundaryLiteralTruth{
		{databasev1.RuntimePluginID, "contracts/schemas/database/v1/contract.go"},
		{databasev1.ConnectionManagerPluginID, "contracts/schemas/database/v1/contract.go"},
		{authenticationv1.BrokerPluginID, "contracts/schemas/authentication/v1/identities.go"},
		{authenticationv1.BrokerCapability, "contracts/schemas/authentication/v1/identities.go"},
		{authenticationv1.OIDCProviderPluginID, "contracts/schemas/authentication/v1/identities.go"},
		{authenticationv1.WebhookDeliveryPluginID, "contracts/schemas/authentication/v1/identities.go"},
		{artifactassessment.AssessmentProviderPluginID, "extensions/libraries/go/artifactassessment/lease.go"},
		{credentiallease.Capability, "extensions/libraries/go/credentiallease/lease.go"},
		{credentiallease.RuntimeKernelService, "extensions/libraries/go/credentiallease/lease.go"},
		{platformcontrolv1.TrustedBootstrapScene, "contracts/schemas/platformcontrol/v1/profile.go"},
		{platformcontrolv1.DatabaseConnectionResourceID, "contracts/schemas/platformcontrol/v1/profile.go"},
		{recordstorev1.SchemaControllerEvidencePrefix, "contracts/schemas/recordstore/v1/contract.go"},
	}
	for _, file := range collectGoFiles(t) {
		if strings.HasSuffix(file.relPath, "_test.go") || file.generated {
			continue
		}
		for _, literal := range goStringLiterals(t, file.relPath) {
			for _, truth := range truths {
				if literal == truth.value && file.relPath != truth.source {
					t.Errorf("可信边界字面量被重复声明: %s 包含 %q\n  唯一真相源: %s", file.relPath, literal, truth.source)
				}
			}
		}
	}
}

func goStringLiterals(t *testing.T, relativePath string) []string {
	t.Helper()
	path := filepath.Join(repoRoot(t), filepath.FromSlash(relativePath))
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("解析 %s: %v", relativePath, err)
	}
	var values []string
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("解析 %s 字符串字面量: %v", relativePath, err)
		}
		values = append(values, value)
		return true
	})
	return values
}
