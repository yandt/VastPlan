package interaction

import (
	"context"
	"testing"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
)

type brokerStub struct {
	opened  interactionapi.Subject
	request uiv1.InteractionRequest
}

func (b *brokerStub) Open(_ context.Context, s interactionapi.Subject, r uiv1.InteractionRequest) (interactionapi.Record, error) {
	b.opened, b.request = s, r
	return interactionapi.Record{Request: r, State: interactionapi.StateCreated, UpdatedAt: time.Unix(1, 0)}, nil
}
func (*brokerStub) Watch(_ context.Context, _ interactionapi.Subject, id string, _ time.Time) (interactionapi.Record, error) {
	response := uiv1.InteractionResponse{InteractionID: id, Decision: uiv1.DecisionAnswered}
	return interactionapi.Record{State: interactionapi.StateAnswered, Response: &response, UpdatedAt: time.Unix(2, 0)}, nil
}
func (*brokerStub) Cancel(context.Context, interactionapi.Subject, string) (interactionapi.Record, error) {
	return interactionapi.Record{}, nil
}

func TestClientUsesBoundRunnerSourceAndBrokerOnly(t *testing.T) {
	broker := &brokerStub{}
	source := interactionapi.Subject{ID: "com.vastplan.runner.workflow", TenantID: "tenant-a"}
	client, err := New(broker, source)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Request(context.Background(), uiv1.InteractionRequest{ID: "interaction-0001", Source: uiv1.InteractionSource{Capability: source.ID}, TenantID: source.TenantID})
	if err != nil || response.Decision != uiv1.DecisionAnswered || broker.opened.ID != source.ID || broker.opened.TenantID != source.TenantID {
		t.Fatalf("Runner 应仅通过绑定 Broker 等待结果: response=%+v opened=%+v err=%v", response, broker.opened, err)
	}
	if _, err := client.Request(context.Background(), uiv1.InteractionRequest{ID: "interaction-0002", Source: uiv1.InteractionSource{Capability: "other"}, TenantID: source.TenantID}); err == nil {
		t.Fatal("Runner 不得伪造来源 capability")
	}
}
