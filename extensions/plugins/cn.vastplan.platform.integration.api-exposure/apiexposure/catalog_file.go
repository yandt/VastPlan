package apiexposure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

// LoadContractCatalogFile reads the trusted-host materialized catalog without
// following symlinks or accepting a file writable by other principals.
func LoadContractCatalogFile(path string) (apiv1.ContractCatalog, error) {
	raw, err := readSecureRegularFile(path, maximumStateBytes)
	if err != nil {
		return apiv1.ContractCatalog{}, fmt.Errorf("读取 API Contract Catalog: %w", err)
	}
	var catalog apiv1.ContractCatalog
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return apiv1.ContractCatalog{}, fmt.Errorf("解析 API Contract Catalog: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return apiv1.ContractCatalog{}, errors.New("API Contract Catalog 只能包含一个 JSON 文档")
	}
	if err := apiv1.ValidateContractCatalog(catalog); err != nil {
		return apiv1.ContractCatalog{}, err
	}
	return catalog, nil
}

func readSecureRegularFile(path string, maximumBytes int64) ([]byte, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || pathInfo.Mode().Perm()&0o022 != 0 || pathInfo.Size() <= 0 || pathInfo.Size() > maximumBytes {
		return nil, errors.New("文件必须是不可由组或其他用户写入的有界普通文件")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("文件在安全检查期间发生变化")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) == 0 || int64(len(raw)) > maximumBytes {
		return nil, errors.New("文件大小超出允许范围")
	}
	return raw, nil
}
