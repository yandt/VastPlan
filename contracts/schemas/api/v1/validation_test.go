package apiv1

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRouteKeyIsOpaqueAndCollisionResistant(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-z2-7]{20}$`)
	seen := map[string]struct{}{}
	for range 256 {
		key, err := NewRouteKey()
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(key) {
			t.Fatalf("Route Key 格式错误: %q", key)
		}
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("Route Key 在测试样本中冲突: %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestContractDigestIsIndependentFromDeclarationOrder(t *testing.T) {
	left := validContract()
	left.Routes[0].Errors = []ErrorMapping{{Code: "platform.demo.conflict", Status: 409}, {Code: "platform.demo.invalid", Status: 422}}
	right := left
	right.Routes = append([]RouteContract(nil), left.Routes...)
	right.Routes[0].Errors = []ErrorMapping{{Code: "platform.demo.invalid", Status: 422}, {Code: "platform.demo.conflict", Status: 409}}
	right.Routes[0].RequestSchema = json.RawMessage("{\n  \"additionalProperties\": false,\n  \"type\": \"object\"\n}")
	leftDigest, err := ContractDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := ContractDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("声明顺序不应改变契约摘要: %s != %s", leftDigest, rightDigest)
	}
}

func TestContractDigestMatchesNodeGateway(t *testing.T) {
	contract := ContractContribution{
		ID: "management-api", ServiceRole: "backend", ContractID: "platform.demo.api",
		ContractVersion: "1.0.0", Protocol: ProtocolHTTPJSON,
		Routes: []RouteContract{{
			ID: "platform.demo.list", Method: "GET", Path: "/items/{itemId}", SuccessStatus: 200,
			Target:        CapabilityTarget{Capability: "platform.demo", Operation: "listItems"},
			RequestSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
			ResponseSchema: json.RawMessage(`{
              "type":"object","additionalProperties":false,
              "properties":{"ok":{"type":"boolean"}},"required":["ok"]
            }`),
			Errors: []ErrorMapping{{Code: "platform.demo.not_found", Status: 404}},
		}},
	}
	digest, err := ContractDigest(contract)
	if err != nil {
		t.Fatal(err)
	}
	const nodeDigest = "f4a83624f391ff26825cddcffb48bfa970687749a3aa3dd2a4f8b38c00dfdc3f"
	if digest != nodeDigest {
		t.Fatalf("Go/Node API Contract 摘要不一致: go=%s node=%s", digest, nodeDigest)
	}
}

func TestContractRejectsExternalSchemaReference(t *testing.T) {
	contract := validContract()
	contract.Routes[0].RequestSchema = json.RawMessage(`{"$ref":"https://attacker.example/schema.json"}`)
	if err := ValidateContractContribution(contract); err == nil || !strings.Contains(err.Error(), "不得引用外部资源") {
		t.Fatalf("外部 Schema 引用必须被拒绝: %v", err)
	}
}

func TestExposureCatalogIsSelfContainedAndRejectsRouteKeyCollision(t *testing.T) {
	resolved := validResolvedExposure(t)
	catalog := ExposureCatalog{SchemaVersion: SchemaVersion, Generation: 1, Exposures: []ResolvedExposure{resolved}}
	if err := ValidateExposureCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveExposure(catalog, "API.EXAMPLE.COM.", resolved.Exposure.RouteKey, 1)
	if !ok || got.Contract.ContractID != resolved.Contract.ContractID {
		t.Fatalf("Gateway 应从自包含目录解析契约: ok=%v got=%+v", ok, got)
	}
	duplicate := resolved
	duplicate.Exposure.ID = "exp_bbbbbbbbbbbbbbbbbbbb"
	catalog.Exposures = append(catalog.Exposures, duplicate)
	if err := ValidateExposureCatalog(catalog); err == nil || !strings.Contains(err.Error(), "routeKey 冲突") {
		t.Fatalf("Route Key 冲突必须 fail-closed: %v", err)
	}
}

func TestEndpointLeaseIsShortLivedHTTPSOnly(t *testing.T) {
	now := time.Now().UTC()
	lease := EndpointLease{
		SchemaVersion: SchemaVersion, LeaseID: "lease_" + strings.Repeat("a", 32),
		DataPlaneExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", InstanceID: "repo-1",
		Endpoint: "https://repo.internal:9443", TLSIdentity: "spiffe://vastplan/data-plane/repo-1",
		Modes: []string{ModeTicketRedirect}, IssuedAt: now, ExpiresAt: now.Add(4 * time.Minute),
	}
	if err := ValidateEndpointLease(lease, now); err != nil {
		t.Fatal(err)
	}
	lease.Endpoint = "https://user:secret@repo.internal:9443"
	if err := ValidateEndpointLease(lease, now); err == nil {
		t.Fatal("携带凭据的 Endpoint Lease 必须被拒绝")
	}
	lease.Endpoint = "https://repo.internal:9443"
	lease.ExpiresAt = now.Add(6 * time.Minute)
	if err := ValidateEndpointLease(lease, now); err == nil {
		t.Fatal("超过最大租期的 Endpoint Lease 必须被拒绝")
	}
}

func validContract() ContractContribution {
	objectSchema := json.RawMessage(`{"type":"object","additionalProperties":false}`)
	return ContractContribution{
		ID: "management-api", ServiceRole: "backend", ContractID: "platform.demo.api",
		ContractVersion: "1.0.0", Protocol: ProtocolHTTPJSON,
		Routes: []RouteContract{{
			ID: "platform.demo.list", Method: "POST", Path: "/items", SuccessStatus: 200,
			Target:        CapabilityTarget{Capability: "platform.demo", Operation: "listItems"},
			RequestSchema: objectSchema, ResponseSchema: objectSchema,
		}},
	}
}

func validResolvedExposure(t *testing.T) ResolvedExposure {
	t.Helper()
	contract := validContract()
	digest, err := ContractDigest(contract)
	if err != nil {
		t.Fatal(err)
	}
	return ResolvedExposure{
		Exposure: Exposure{
			SchemaVersion: SchemaVersion, ID: "exp_aaaaaaaaaaaaaaaaaaaa", Revision: 1,
			RouteKey: "aaaaaaaaaaaaaaaaaaaa", DisplayName: "演示 API", TenantID: "tenant-a",
			Hosts: []string{"api.example.com"},
			Contract: ContractReference{
				PluginID: "cn.vastplan.platform.demo", ArtifactSHA256: strings.Repeat("a", 64),
				ContributionID: contract.ID, ContractID: contract.ContractID,
				ContractVersion: contract.ContractVersion, ContractDigest: digest,
			},
			Authentication:      AuthenticationPolicy{ProfileID: "auth.default", AllowAnonymous: false},
			RequiredPermissions: []string{"platform.demo.read"},
			Limits:              ExposureLimits{MaxBodyBytes: 1024, MaxResponseBytes: 4096, RequestsPerMinute: 60, TimeoutMS: 5000},
			Target:              ExposureTarget{LogicalService: "backend.default", RoutingDomain: "platform.default"},
		},
		Contract: contract,
	}
}
