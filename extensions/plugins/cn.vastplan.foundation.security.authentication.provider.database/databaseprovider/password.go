package databaseprovider

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

func VerifyArgon2id(encoded string, password []byte) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, timeCost uint64
	var threads uint64
	for _, item := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return false
		}
		number, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return false
		}
		switch key {
		case "m":
			memory = number
		case "t":
			timeCost = number
		case "p":
			threads = number
		default:
			return false
		}
	}
	if memory < 8*1024 || memory > 1024*1024 || timeCost < 1 || timeCost > 10 || threads < 1 || threads > 32 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	actual := argon2.IDKey(password, salt, uint32(timeCost), uint32(memory), uint8(threads), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func EncodeArgon2id(password, salt []byte) (string, error) {
	if len(password) < 12 || len(salt) != 16 {
		return "", errors.New("Argon2id 输入无效")
	}
	hash := argon2.IDKey(password, salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
