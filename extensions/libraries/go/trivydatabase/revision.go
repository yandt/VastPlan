// Package trivydatabase defines the deterministic content identity shared by
// Trivy snapshot producers and consumers without owning filesystem access.
package trivydatabase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// Revision computes the stable identity of metadata.json and trivy.db. The
// caller owns opening, permission-checking, sizing, and closing both readers.
func Revision(metadata, database io.Reader) (string, error) {
	if metadata == nil || database == nil {
		return "", errors.New("Trivy 数据库摘要输入不能为空")
	}
	hash := sha256.New()
	for _, file := range []struct {
		name   string
		reader io.Reader
	}{{"db/metadata.json", metadata}, {"db/trivy.db", database}} {
		_, _ = io.WriteString(hash, file.name+"\x00")
		if _, err := io.Copy(hash, file.reader); err != nil {
			return "", err
		}
		_, _ = io.WriteString(hash, "\x00")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
