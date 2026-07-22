package seedaccess

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/argon2"
)

type PasswordVerifier struct {
	Algorithm string `json:"algorithm"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memoryKiB"`
	Threads   uint8  `json:"threads"`
	Salt      string `json:"salt"`
	Hash      string `json:"hash"`
}

func NewPasswordVerifier(password []byte) (PasswordVerifier, error) {
	return newPasswordVerifier(password, 3, 64*1024, 2)
}

func newPasswordVerifier(password []byte, rounds, memory uint32, threads uint8) (PasswordVerifier, error) {
	if len(password) < 12 || len(password) > 1024 {
		return PasswordVerifier{}, errors.New("Seed Operator 密码长度必须为 12-1024 字节")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return PasswordVerifier{}, err
	}
	hash := argon2.IDKey(password, salt, rounds, memory, threads, 32)
	return PasswordVerifier{Algorithm: "argon2id", Time: rounds, MemoryKiB: memory, Threads: threads, Salt: base64.RawStdEncoding.EncodeToString(salt), Hash: base64.RawStdEncoding.EncodeToString(hash)}, nil
}

func (v PasswordVerifier) Verify(password []byte) bool {
	if v.Algorithm != "argon2id" || v.Time < 1 || v.MemoryKiB < 8*1024 || v.Threads < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(v.Salt)
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(v.Hash)
	if err != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey(password, salt, v.Time, v.MemoryKiB, v.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
