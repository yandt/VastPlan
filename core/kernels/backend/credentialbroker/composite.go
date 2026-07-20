package credentialbroker

import (
	"context"
	"errors"

	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
)

// Composite routes managed handles to the encrypted lease broker and legacy
// named bootstrap references to the protected directory broker. Ambiguous
// references fail closed.
type Composite struct {
	managed kernelspi.CredentialBroker
	named   kernelspi.CredentialBroker
}

func NewComposite(managed, named kernelspi.CredentialBroker) (*Composite, error) {
	if managed == nil && named == nil {
		return nil, errors.New("组合凭证 broker 至少需要一个后端")
	}
	return &Composite{managed: managed, named: named}, nil
}

func (b *Composite) WithCredential(ctx context.Context, scope kernelspi.Scope, ref kernelspi.CredentialRef, use func(kernelspi.CredentialMaterial) error) error {
	managed, named := ref.Handle != "", ref.Name != ""
	if managed == named {
		return errors.New("CredentialRef 必须且只能包含 handle 或 name")
	}
	if managed {
		if b.managed == nil {
			return errors.New("托管凭证 broker 不可用")
		}
		return b.managed.WithCredential(ctx, scope, ref, use)
	}
	if b.named == nil {
		return errors.New("命名凭证 broker 不可用")
	}
	return b.named.WithCredential(ctx, scope, ref, use)
}
