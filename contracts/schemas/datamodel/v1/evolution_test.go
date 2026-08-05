package datamodelv1

import "testing"

func TestClassifyEvolution(t *testing.T) {
	base := Model{Contract: "data.model.v1", ID: "example.order", SchemaVersion: 1,
		Storage:    StorageBinding{Kind: "connection-ref", Table: "orders"},
		Fields:     []Field{{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"}},
		PrimaryKey: []string{"id"}, Indexes: []Index{}, UniqueConstraints: []UniqueConstraint{},
		Scope: Scope{Tenant: "none", Service: "none"}, Deletion: DeletionPolicy{Mode: "hard"}}
	if got := ClassifyEvolution(nil, base); got.Kind != EvolutionCreate {
		t.Fatalf("new model kind = %s", got.Kind)
	}
	additive := base
	additive.SchemaVersion = 2
	additive.Fields = append(append([]Field(nil), base.Fields...), Field{ID: "note", Column: "note", Type: "string", Nullable: true, Sensitivity: "internal"})
	if got := ClassifyEvolution(&base, additive); got.Kind != EvolutionAdditive || len(got.AddedFields) != 1 {
		t.Fatalf("additive evolution = %+v", got)
	}
	destructive := additive
	destructive.SchemaVersion = 3
	destructive.Fields = append([]Field(nil), base.Fields...)
	if got := ClassifyEvolution(&additive, destructive); got.Kind != EvolutionManual {
		t.Fatalf("destructive evolution = %+v", got)
	}
}
