package platformcontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
)

const maxSecretBytes = 64 << 10

type SecretResolver func(platformcontrolv1.SecretRef) (SecretSource, error)

// ResolveSecretSource selects one trusted bootstrap secret implementation at
// the composition boundary. Callers use only SecretSource afterwards.
func ResolveSecretSource(ref platformcontrolv1.SecretRef, credentialsDirectory string) (SecretSource, error) {
	if err := platformcontrolv1.ValidateSecretRef(ref); err != nil {
		return nil, err
	}
	switch ref.Kind {
	case "owner-file":
		return &fileSecretSource{path: ref.Path, requireOwnerOnly: true}, nil
	case "systemd-credential":
		if !filepath.IsAbs(credentialsDirectory) || filepath.Clean(credentialsDirectory) != credentialsDirectory {
			return nil, errors.New("CREDENTIALS_DIRECTORY 不可用")
		}
		return &fileSecretSource{path: filepath.Join(credentialsDirectory, ref.Name)}, nil
	default:
		return nil, errors.New("Bootstrap Secret Provider 不受支持")
	}
}

type fileSecretSource struct {
	path             string
	requireOwnerOnly bool
}

func (s *fileSecretSource) WithSecret(ctx context.Context, use func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(s.path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || s.requireOwnerOnly && info.Mode().Perm()&0o077 != 0 {
		return errors.New("Bootstrap secret 必须是受保护的普通文件")
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	defer clear(raw)
	if len(raw) == 0 || len(raw) > maxSecretBytes || use == nil {
		return errors.New("Bootstrap secret 内容无效")
	}
	return use(raw)
}
