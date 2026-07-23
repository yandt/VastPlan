package pluginservice

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func writeImmutableSidecar(filename string, raw []byte, label string) error {
	if len(raw) == 0 {
		return nil
	}
	if existing, err := os.ReadFile(filename); err == nil {
		if !bytes.Equal(existing, raw) {
			return fmt.Errorf("同一不可变制品已经存在不同的%s", label)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取既有%s: %w", label, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("%s不能为空", label)
	}
	if err := writeFileAtomically(filename, append([]byte(nil), raw...), 0o644); err != nil {
		return fmt.Errorf("写入%s: %w", label, err)
	}
	return nil
}

func readProvenanceSidecars(directory string) ([]byte, []byte, error) {
	provenance, err := readOptionalSidecar(filepath.Join(directory, "provenance.dsse.json"), "来源证明")
	if err != nil {
		return nil, nil, err
	}
	verification, err := readOptionalSidecar(filepath.Join(directory, "provenance-verification.json"), "来源证明验证记录")
	if err != nil {
		return nil, nil, err
	}
	if (len(provenance) == 0) != (len(verification) == 0) {
		return nil, nil, errors.New("原始来源证明与验证记录必须同时存在")
	}
	return provenance, verification, nil
}

func readOptionalSidecar(filename, label string) ([]byte, error) {
	raw, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取%s: %w", label, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%s不能为空", label)
	}
	return raw, nil
}
