package pluginconfiguration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateValuesUsesFrozenClosedSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["region"],"properties":{"region":{"type":"string","minLength":2}}}`)
	digest, err := digestRawJSON(schema)
	if err != nil {
		t.Fatal(err)
	}
	definition := Definition{Schema: schema, SchemaDigest: digest}
	if err := ValidateValues(definition, []byte(`{"region":"cn-east"}`)); err != nil {
		t.Fatalf("合法配置应通过: %v", err)
	}
	for _, invalid := range []json.RawMessage{
		[]byte(`{"region":"x"}`),
		[]byte(`{"region":"cn-east","token":"secret"}`),
		[]byte(`[]`),
		[]byte(strings.Repeat(" ", MaxValuesBytes+1)),
	} {
		if err := ValidateValues(definition, invalid); err == nil {
			t.Fatalf("非法配置必须拒绝: %s", invalid)
		}
	}
}
