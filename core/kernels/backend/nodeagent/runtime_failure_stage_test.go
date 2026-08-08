package nodeagent

import "testing"

func TestSchemaActivationFailureHasDedicatedRuntimeStage(t *testing.T) {
	err := &SchemaActivationError{ModelID: "example.order", Phase: "verify", Err: assertError("drift")}
	if got := runtimeFailureStage(err); got != "schema_verify" {
		t.Fatalf("runtime failure stage = %s", got)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
