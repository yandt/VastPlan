package versioningv1_test

import (
	"encoding/json"
	"os"
	"testing"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func TestVersionIdentityGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/version-identity-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Algorithm string `json:"algorithm"`
		Vectors   []struct {
			Name           string `json:"name"`
			TenantID       string `json:"tenantId"`
			Namespace      string `json:"namespace"`
			StreamID       string `json:"streamId"`
			IdempotencyKey string `json:"idempotencyKey"`
			VersionID      string `json:"versionId"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Algorithm != versioningv1.VersionIdentityAlgorithm {
		t.Fatalf("identity algorithm 漂移: %s", document.Algorithm)
	}
	for _, vector := range document.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			stream := versioningv1.StreamKey{Namespace: vector.Namespace, StreamID: vector.StreamID}
			versionID, err := versioningv1.DeriveVersionID(vector.TenantID, stream, vector.IdempotencyKey)
			if err != nil {
				t.Fatal(err)
			}
			if versionID != vector.VersionID {
				t.Fatalf("versionId 漂移: want %s, got %s", vector.VersionID, versionID)
			}
			if err := versioningv1.ValidateDerivedVersionID(versionID, vector.TenantID, stream, vector.IdempotencyKey); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVersionIdentityRejectsAmbiguousInputs(t *testing.T) {
	stream := versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "portal-main"}
	for name, tenant := range map[string]string{
		"empty": "", "leading whitespace": " tenant-a", "nul": "tenant\x00a",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := versioningv1.DeriveVersionID(tenant, stream, "portal-main:revision:0001"); err == nil {
				t.Fatalf("歧义 tenant 必须拒绝: %q", tenant)
			}
		})
	}
	valid, err := versioningv1.DeriveVersionID("tenant-a", stream, "portal-main:revision:0001")
	if err != nil {
		t.Fatal(err)
	}
	if err := versioningv1.ValidateDerivedVersionID(valid, "tenant-b", stream, "portal-main:revision:0001"); err == nil {
		t.Fatal("versionId 不得跨 tenant 复用")
	}
	if _, err := versioningv1.DeriveVersionID("tenant-a", stream, "short"); err == nil {
		t.Fatal("非法 idempotencyKey 必须拒绝")
	}
}
