package operationfence

import (
	"context"
	"testing"
)

func TestEvidenceIsHostOnlyAndBuildsStableFence(t *testing.T) {
	evidence := Evidence{LogicalService: "platform.deployment", UnitID: "platform-deployment", Epoch: 7, Token: "opaque-token"}
	ctx, err := WithEvidence(context.Background(), evidence)
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := FromContext(ctx)
	if !ok || loaded != evidence {
		t.Fatalf("host-only evidence 丢失: %+v", loaded)
	}
	fence, err := loaded.ForOperation("bootstrap/job-a")
	if err != nil || fence.Epoch != 7 || fence.OperationID != "bootstrap/job-a" {
		t.Fatalf("操作 fence 无效: %+v err=%v", fence, err)
	}
}
