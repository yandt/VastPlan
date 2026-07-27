package trivydatabase

import (
	"strings"
	"testing"
)

func TestRevisionBindsBothNamedInputs(t *testing.T) {
	first, err := Revision(strings.NewReader("metadata"), strings.NewReader("database"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Revision(strings.NewReader("metadata-2"), strings.NewReader("database"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || first == second {
		t.Fatalf("Trivy revision 未绑定两个输入: first=%s second=%s", first, second)
	}
}

func TestRevisionRejectsMissingReader(t *testing.T) {
	if _, err := Revision(nil, strings.NewReader("database")); err == nil {
		t.Fatal("缺失 metadata reader 必须失败")
	}
}
