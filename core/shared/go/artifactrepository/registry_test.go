package artifactrepository

import (
	"context"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

type stubAdapter struct{ profile artifactrepositoryv1.Profile }

func (a stubAdapter) Profile() artifactrepositoryv1.Profile { return a.profile }
func (stubAdapter) ReadExact(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	return artifacttrust.Envelope{}, nil
}
func (stubAdapter) Publish(context.Context, artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	return artifactrepositoryv1.Receipt{}, nil
}
func (stubAdapter) CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	return artifactrepositoryv1.CatalogSnapshot{}, nil
}
func TestRegistryRequiresExactProtocolAndProfile(t *testing.T) {
	profile, err := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		Endpoint: "unix:///tmp/vastplan/repository.sock", Channels: []string{"testing"}, DevelopmentOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if _, err := registry.Open(profile); err == nil {
		t.Fatal("未注册协议不得回退其他 Adapter")
	}
	if err := registry.Register(artifactrepositoryv1.ProtocolLocalTest, func(got artifactrepositoryv1.Profile) (Adapter, error) { return stubAdapter{profile: got}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Open(profile); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(artifactrepositoryv1.ProtocolLocalTest, func(profile artifactrepositoryv1.Profile) (Adapter, error) { return stubAdapter{profile}, nil }); err == nil {
		t.Fatal("同一协议不得被覆盖注册")
	}
}
