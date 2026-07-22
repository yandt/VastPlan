package authorizationv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAllProviderProtocolsExposeOnlyRegisteredOperations(t *testing.T) {
	expected := map[string]int{
		authorizationv1.ProtocolStore: 6, authorizationv1.ProtocolEngine: 4,
		authorizationv1.ProtocolDirectory: 3, authorizationv1.ProtocolExchange: 4,
	}
	for protocol, count := range expected {
		operations := authorizationv1.ProtocolOperations(protocol)
		if len(operations) != count {
			t.Fatalf("%s 操作数异常: %v", protocol, operations)
		}
		for _, operation := range operations {
			if !authorizationv1.KnownProtocolOperation(protocol, operation) {
				t.Fatalf("登记操作无法查询: %s/%s", protocol, operation)
			}
		}
	}
	if _, err := authorizationv1.ParseProviderRequest(authorizationv1.ProtocolStore, "publish", []byte(`{}`)); err == nil {
		t.Fatal("Store Provider 不得拥有发布策略操作")
	}
	offered := []authorizationv1.ProtocolSupport{{Protocol: authorizationv1.ProtocolEngine, Operations: authorizationv1.ProtocolOperations(authorizationv1.ProtocolEngine)}}
	if _, err := authorizationv1.NegotiateProtocol(authorizationv1.ProtocolEngine, offered); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizationv1.NegotiateProtocol("authorization.engine.v2", offered); err == nil {
		t.Fatal("协议协商不得静默降级 major version")
	}
}

func TestStoreCASIsStrictOpaqueAndContentBound(t *testing.T) {
	content := json.RawMessage(`{"roles":[]}`)
	digest, err := authorizationv1.DigestRawDocument(content)
	if err != nil {
		t.Fatal(err)
	}
	request := authorizationv1.StoreCompareAndSwapRequest{
		DomainID: "platform.root", ExpectedRevision: 4, IdempotencyKey: "request-0000000001",
		Document: authorizationv1.StoreDocument{Format: "vastplan.authorization.state", SchemaVersion: "v1", Revision: 5, Digest: digest, Content: content},
	}
	if _, err := authorizationv1.ParseProviderRequest(authorizationv1.ProtocolStore, "compareAndSwap", marshal(t, request)); err != nil {
		t.Fatal(err)
	}
	request.Document.Digest = strings.Repeat("0", 64)
	if _, err := authorizationv1.ParseProviderRequest(authorizationv1.ProtocolStore, "compareAndSwap", marshal(t, request)); err == nil {
		t.Fatal("Store document 摘要不匹配必须拒绝")
	}
	raw := marshal(t, authorizationv1.StoreLoadRequest{DomainID: "platform.root", MinRevision: 1})
	raw = append(raw[:len(raw)-1], []byte(`,"allow":true}`)...)
	if _, err := authorizationv1.ParseProviderRequest(authorizationv1.ProtocolStore, "load", raw); err == nil {
		t.Fatal("Provider 私有扩展字段必须被拒绝")
	}
}

func TestEngineDecisionProofIsBoundedAndConsistent(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	proof := authorizationv1.DecisionProof{
		ProofID: "proof.000000000001", ProviderID: "native-rbac", PolicyDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64),
		Decision: authorizationv1.DecisionAllow, ReasonCode: "authorization.permission.matched", MatchedRoleIDs: []string{"orders.viewer"}, MatchedBindingIDs: []string{"binding.alice.viewer"}, RevocationRevision: 2, EvaluatedAt: now, ValidUntil: now.Add(time.Minute),
	}
	result := authorizationv1.EngineEvaluateResult{Decision: authorizationv1.DecisionAllow, Proof: proof}
	if _, err := authorizationv1.ParseProviderResult(authorizationv1.ProtocolEngine, "evaluate", marshal(t, result)); err != nil {
		t.Fatal(err)
	}
	result.Decision = authorizationv1.DecisionDeny
	if _, err := authorizationv1.ParseProviderResult(authorizationv1.ProtocolEngine, "evaluate", marshal(t, result)); err == nil {
		t.Fatal("decision 与 proof 不一致必须拒绝")
	}
	result.Decision = authorizationv1.DecisionAllow
	result.Proof.ValidUntil = now.Add(6 * time.Minute)
	if _, err := authorizationv1.ParseProviderResult(authorizationv1.ProtocolEngine, "evaluate", marshal(t, result)); err == nil {
		t.Fatal("Provider 不得返回超出协议上限的 Decision Proof")
	}
}

func TestDirectoryAndExchangeCannotGrantDirectly(t *testing.T) {
	groups := authorizationv1.DirectoryResolveGroupsResult{Groups: []authorizationv1.ExternalGroup{{ID: "ops", Issuer: "https://idp.example.test"}, {ID: "ops", Issuer: "https://idp.example.test"}}, DirectoryRevision: 1}
	if _, err := authorizationv1.ParseProviderResult(authorizationv1.ProtocolDirectory, "resolveGroups", marshal(t, groups)); err == nil {
		t.Fatal("Directory 重复组必须拒绝")
	}
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	proposal := authorizationv1.PolicyImportProposal{
		DomainID: "tenant.acme", SourceDigest: strings.Repeat("d", 64),
		Roles:    []authorizationv1.CompiledRole{{ID: "imported.viewer", Revision: 1, DomainID: "tenant.other", Statements: []authorizationv1.PolicyStatement{{ID: "read", Effect: authorizationv1.EffectAllow, Permissions: []string{"tenant.orders.read"}, Constraints: []authorizationv1.AttributeConstraint{}}}}},
		Bindings: []authorizationv1.SubjectBinding{{ID: "imported.binding", Revision: 1, DomainID: "tenant.acme", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: "alice", Issuer: "https://idp.example.test"}, RoleID: "imported.viewer", RoleRevision: 1, NotBefore: now, ExpiresAt: now.Add(time.Hour)}},
	}
	if _, err := authorizationv1.ParseProviderResult(authorizationv1.ProtocolExchange, "import", marshal(t, authorizationv1.ExchangeImportResult{Proposal: proposal})); err == nil {
		t.Fatal("Exchange Provider 不得跨 Domain 注入 Role")
	}
}

func TestProviderDescriptorRejectsUnknownSemanticsAndExternalSchema(t *testing.T) {
	descriptor := authorizationv1.ProviderDescriptor{
		ProviderID: "native-rbac", PluginID: "cn.vastplan.foundation.security.authorization-engine", Version: "1.0.0",
		Protocols:           []authorizationv1.ProtocolSupport{{Protocol: authorizationv1.ProtocolEngine, Operations: authorizationv1.ProtocolOperations(authorizationv1.ProtocolEngine)}},
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}
	if err := authorizationv1.ValidateProviderDescriptor(descriptor); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizationv1.ParseProviderDescriptor(marshal(t, descriptor)); err != nil {
		t.Fatal(err)
	}
	descriptor.Protocols[0].Operations = append(descriptor.Protocols[0].Operations, "publish")
	if err := authorizationv1.ValidateProviderDescriptor(descriptor); err == nil {
		t.Fatal("未知 Provider 操作必须拒绝")
	}
	descriptor.Protocols[0].Operations = authorizationv1.ProtocolOperations(authorizationv1.ProtocolEngine)
	descriptor.Protocols[0].Operations = descriptor.Protocols[0].Operations[:2]
	if err := authorizationv1.ValidateProviderDescriptor(descriptor); err == nil {
		t.Fatal("Provider 声明协议时必须实现完整 operation 集")
	}
	descriptor.Protocols[0].Operations = authorizationv1.ProtocolOperations(authorizationv1.ProtocolEngine)
	descriptor.ConfigurationSchema = json.RawMessage(`{"type":"object","properties":{"policy":{"$ref":"https://provider.invalid/policy.json"}}}`)
	if err := authorizationv1.ValidateProviderDescriptor(descriptor); err == nil {
		t.Fatal("Provider 配置不得加载外部 Schema")
	}
	descriptor.ConfigurationSchema = json.RawMessage(`{"type":"object","properties":{"apiToken":{"type":"string"}}}`)
	if err := authorizationv1.ValidateProviderDescriptor(descriptor); err == nil {
		t.Fatal("Provider 非敏感配置不得声明秘密字段")
	}
}
