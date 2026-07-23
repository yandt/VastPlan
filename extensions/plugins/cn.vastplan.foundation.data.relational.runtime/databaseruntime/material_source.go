package databaseruntime

import (
	"context"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	credentialmaterial "cdsoft.com.cn/VastPlan/extensions/sdk/go/credentialmaterial"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// RuntimeMaterialSource asks the local Kernel to relay a lease encrypted to
// this process' one-use key. The Kernel sees only the CredentialRef, public key
// and ciphertext; Open runs exclusively inside Database Runtime.
type RuntimeMaterialSource struct {
	source *credentialmaterial.Source
	now    func() time.Time
}

func NewRuntimeMaterialSource(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef, audience string) (*RuntimeMaterialSource, error) {
	source, err := credentialmaterial.New(host, tenant, ref, audience)
	if err != nil {
		return nil, err
	}
	return &RuntimeMaterialSource{source: source, now: time.Now}, nil
}

// NewRuntimeMaterialSourceFromEnvironment reads the audience injected by the
// trusted Host. It is non-secret but reserved and cannot be inherited or
// overridden by a plugin manifest or execution driver.
func NewRuntimeMaterialSourceFromEnvironment(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef) (*RuntimeMaterialSource, error) {
	source, err := credentialmaterial.NewFromEnvironment(host, tenant, ref)
	if err != nil {
		return nil, err
	}
	return &RuntimeMaterialSource{source: source, now: time.Now}, nil
}

func (s *RuntimeMaterialSource) WithMaterial(ctx context.Context, use func(CredentialMaterial) error) error {
	return s.source.WithMaterial(ctx, s.now().UTC(), func(material credentialmaterial.Material) error { return use(material) })
}
