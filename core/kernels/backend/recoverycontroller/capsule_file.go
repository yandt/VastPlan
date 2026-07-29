package recoverycontroller

import (
	"errors"
	"os"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
)

const maxCapsuleBytes = 1 << 20

func LoadCapsule(filename string) (recoveryv1.Capsule, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return recoveryv1.Capsule{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > maxCapsuleBytes {
		return recoveryv1.Capsule{}, errors.New("Recovery Capsule 必须是不可被 group/other 写入的有界普通文件")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return recoveryv1.Capsule{}, err
	}
	return recoveryv1.ParseCapsule(raw)
}
