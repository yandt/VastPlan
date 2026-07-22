package seedaccess

import (
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
)

// FileRecoveryProofVerifier is a narrow adapter for an OS-provisioned local
// recovery secret. The proof file is never copied into Seed State.
type FileRecoveryProofVerifier struct{ Path string }

func (v FileRecoveryProofVerifier) VerifyLocalRecoveryProof(proof []byte) error {
	if !filepath.IsAbs(v.Path) || filepath.Clean(v.Path) != v.Path {
		return errors.New("本机恢复证明必须使用规范绝对路径")
	}
	info, err := os.Lstat(v.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("本机恢复证明必须是 owner-only 普通文件")
	}
	want, err := os.ReadFile(v.Path)
	if err != nil {
		return err
	}
	defer clear(want)
	if len(want) < 32 || len(want) != len(proof) || subtle.ConstantTimeCompare(want, proof) != 1 {
		return errors.New("本机恢复证明不匹配")
	}
	return nil
}
