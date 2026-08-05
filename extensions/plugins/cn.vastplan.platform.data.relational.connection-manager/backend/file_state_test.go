package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// fileStateOperation exists only as a test Provider. Production always binds
// the service to the host-injected, fenced Shared State protocol.
type fileStateOperation struct{ path string }

func newService(path string) (*service, error) {
	if path == "" {
		return nil, errors.New("测试状态文件不能为空")
	}
	value := &service{data: emptyPersisted(), state: &fileStateOperation{path: path}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodePersisted(raw, &value.data); err != nil {
		return nil, err
	}
	return value, nil
}

func (o *fileStateOperation) begin(_ *service, _ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ string) error {
	return nil
}

func (o *fileStateOperation) end(_ *service) {}

func (o *fileStateOperation) save(raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(o.path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(o.path), ".connections-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, o.path)
}
