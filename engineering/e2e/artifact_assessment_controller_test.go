//go:build e2e

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/operationfence"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type currentAssessmentFence struct{ evidence operationfence.Evidence }

func (f currentAssessmentFence) Current() (operationfence.Evidence, bool) { return f.evidence, true }

type assessmentWorkflowFixture struct {
	t              *testing.T
	now            time.Time
	privateKey     ed25519.PrivateKey
	entry          platformadminapi.ArtifactCatalogEntry
	mu             sync.Mutex
	appended       []artifactassessment.AppendStatusRequest
	trustedCalls   int
	appendSignal   chan struct{}
	appendSignaled bool
}

func (f *assessmentWorkflowFixture) forward(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if call.GetTenantId() != "tenant-assessment" || call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != artifactassessment.AssessmentControllerPluginID {
		f.t.Errorf("后台 HostCall 未使用宿主绑定身份: %+v", call)
	}
	if call.GetPrincipal() != nil || len(call.GetCredentials()) != 0 || len(call.GetMetadata()) != 0 || call.GetScene() != "system.background" {
		f.t.Errorf("后台 HostCall 携带了越权上下文: %+v", call)
	}
	f.mu.Lock()
	f.trustedCalls++
	f.mu.Unlock()
	operation := target.GetOperation()
	switch target.GetCapability() + "/" + operation {
	case "platform.artifacts.assessment/status":
		return okJSON(artifactassessment.ProviderRuntimeStatus{
			SchemaVersion:      artifactassessment.SchemaVersion,
			Scanner:            artifactassessment.Scanner{ID: "fixture", Version: "1.0.0", DatabaseRevision: "db-fixture-1"},
			AssessmentRevision: strings.Repeat("9", 64),
		})
	case "platform.artifacts.repository/listCatalog":
		return okJSON(platformadminapi.ArtifactCatalogPage{Revision: 7, Total: 1, Page: 1, PageSize: 10, Items: []platformadminapi.ArtifactCatalogEntry{f.entry}})
	case "platform.artifacts.assessment/assessStatus":
		var request artifactassessment.ProviderStatusRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, fmt.Errorf("解析复扫请求: %w", err)
		}
		record, err := artifactassessment.SignStatus(artifactassessment.StatusRecord{
			AdmissionSHA256: request.AdmissionSHA256,
			Sequence:        request.Sequence,
			PreviousSHA256:  request.PreviousSHA256,
			Evaluation: artifactassessment.Evaluation{
				SubjectSHA256: request.SubjectSHA256,
				SBOMSHA256:    request.SBOMSHA256,
				Scanner:       artifactassessment.Scanner{ID: "fixture", Version: "1.0.0", DatabaseRevision: "db-fixture-1"},
				Decision:      artifactassessment.DecisionPass, EvaluatedAt: f.now, ExpiresAt: f.now.Add(2 * time.Hour),
			},
			ProviderID: "fixture-provider", KeyID: "fixture-key", PolicyID: request.PolicyID,
		}, f.privateKey)
		if err != nil {
			return nil, nil, fmt.Errorf("签署夹具 StatusRecord: %w", err)
		}
		return okJSON(record)
	case "platform.artifacts.repository/appendAssessmentStatus":
		var request artifactassessment.AppendStatusRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, fmt.Errorf("解析追加请求: %w", err)
		}
		if request.Ref != f.entry.Ref {
			f.t.Errorf("追加 ref 漂移: %+v", request.Ref)
		}
		status, _, err := artifactassessment.InspectStatus(request.Record)
		if err != nil || status.Sequence != 1 || status.PreviousSHA256 != f.entry.SecurityAdmission.AdmissionSHA256 {
			f.t.Errorf("追加记录未绑定原链头: status=%+v err=%v", status, err)
		}
		f.mu.Lock()
		f.appended = append(f.appended, request)
		if !f.appendSignaled {
			close(f.appendSignal)
			f.appendSignaled = true
		}
		f.mu.Unlock()
		return okJSON(map[string]any{"sequence": 1})
	default:
		return nil, nil, fmt.Errorf("未预期的评估调用: %s/%s", target.GetCapability(), operation)
	}
}

func okJSON(value any) (*contractv1.CallResult, []byte, error) {
	raw, err := json.Marshal(value)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
}

func TestArtifactAssessmentControllerRealProcessRunsAutonomousWorkflow(t *testing.T) {
	server := startE2ENATS(t)
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sharedstate.NewNATSStore(buckets.SharedState)
	if err != nil {
		t.Fatal(err)
	}
	host, err := hostfactory.NewWithDependencies("0.1.0", t.Logf, kernelspi.Dependencies{SharedState: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	allowAllPermissions(t, host)
	host.SetExecutionFenceProvider(currentAssessmentFence{operationfence.Evidence{
		LogicalService: "platform.artifacts.assessment", UnitID: "assessment-controller", Epoch: 1, Token: "assessment-fence-1",
	}})

	now := time.Now().UTC().Truncate(time.Second)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &assessmentWorkflowFixture{t: t, now: now, privateKey: privateKey, appendSignal: make(chan struct{}), entry: platformadminapi.ArtifactCatalogEntry{
		Ref:    pluginv1.ArtifactRef{PluginID: "com.example.assessed", Version: "1.0.0", Channel: "testing"},
		SHA256: strings.Repeat("a", 64), LifecycleStatus: "active",
		SBOM: &platformadminapi.ArtifactSBOMDeclaration{Format: "cyclonedx-json", SpecVersion: "1.5", SHA256: strings.Repeat("b", 64)},
		SecurityAdmission: &platformadminapi.ArtifactSecurityAdmissionDeclaration{
			AdmissionSHA256: strings.Repeat("c", 64), ProviderID: "fixture-provider", KeyID: "fixture-key", PolicyID: "testing-policy",
			ScannerID: "fixture", ScannerVersion: "1.0.0", DatabaseRevision: "db-admission", Decision: artifactassessment.DecisionPass,
			EvaluatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano),
		},
	}}
	host.SetCapabilityForwarder(fixture.forward)

	root := repoRoot(t)
	manifestRaw, err := os.ReadFile(filepath.Join(root, "extensions/plugins/cn.vastplan.platform.artifacts.assessment.controller/vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	configuration := []byte(`{"tenantId":"tenant-assessment","channels":["testing"],"intervalSeconds":10,"leadTimeSeconds":60,"jitterSeconds":0,"retryBaseSeconds":1,"retryMaxSeconds":2,"pageSize":10}`)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	instance, err := host.LaunchWithPolicy(ctx, buildPlugin(t, "./extensions/plugins/cn.vastplan.platform.artifacts.assessment.controller/backend"), protocolbus.LaunchPolicy{
		PluginID: artifactassessment.AssessmentControllerPluginID, Publisher: "vastplan", Version: manifest.Version,
		ArtifactSHA256: strings.Repeat("d", 64), NodeID: "node-assessment", RuntimeInstanceID: "runtime-assessment", RuntimeScope: "assessment-controller",
		Contributions: contributions, KernelServices: append([]string(nil), manifest.Capabilities.KernelServices...),
		ContextAccess: pluginv1.ContextAccessContract(manifest), Configuration: configuration,
		BackgroundService: true, AutonomousTenantID: "tenant-assessment",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close(instance) }()

	select {
	case <-fixture.appendSignal:
	case <-ctx.Done():
		t.Fatalf("真实 Controller 后台循环未完成复扫追加: %v", ctx.Err())
	}
	for {
		response, invokeErr := host.Invoke(ctx, toolTarget("platform.artifacts.assessment.controller", "status"), &contractv1.CallContext{
			Principal: &contractv1.Principal{UserId: "operator", TenantId: "tenant-assessment"},
			Caller:    &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "e2e"},
			Scene:     "system.test", TenantId: "tenant-assessment",
		}, nil)
		var stats struct {
			Succeeded int `json:"succeeded"`
		}
		if invokeErr == nil && response.GetResult().GetStatus() == contractv1.CallResult_STATUS_OK && json.Unmarshal(response.Payload, &stats) == nil && stats.Succeeded == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Controller 追加后未持久化成功计划: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.appended) != 1 || fixture.trustedCalls < 4 {
		t.Fatalf("后台复扫闭环不完整: appended=%d trustedCalls=%d", len(fixture.appended), fixture.trustedCalls)
	}
}
