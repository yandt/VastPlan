package signingkey

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func Generate(privateOutput string) (ed25519.PublicKey, error) {
	if privateOutput == "" {
		return nil, errors.New("私钥输出路径不能为空")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	privatePEM, err := pluginservice.MarshalEd25519PrivateKeyPEM(privateKey)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(privateOutput), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(privateOutput, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建私钥文件（不会覆盖已有密钥）: %w", err)
	}
	if _, err := file.Write(privatePEM); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return publicKey, nil
}
