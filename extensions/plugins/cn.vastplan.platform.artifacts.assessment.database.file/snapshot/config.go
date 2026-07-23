// Package snapshot atomically materializes one pinned Trivy database revision
// from a private local staging directory.
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

const (
	PluginID      = "cn.vastplan.platform.artifacts.assessment.database.file"
	PluginVersion = "0.1.0"
	Capability    = "platform.artifacts.assessment.database.file"
)

type Config struct {
	SourceDirectory  string `json:"sourceDirectory"`
	SnapshotRoot     string `json:"snapshotRoot"`
	DatabaseRevision string `json:"databaseRevision"`
}

func (c Config) Validate() error {
	for _, path := range []string{c.SourceDirectory, c.SnapshotRoot} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("Trivy database file snapshot 路径必须是规范绝对路径")
		}
	}
	raw, err := hex.DecodeString(c.DatabaseRevision)
	if err != nil || len(raw) != sha256.Size || c.DatabaseRevision != hex.EncodeToString(raw) {
		return errors.New("Trivy database file snapshot revision 必须是规范 SHA-256")
	}
	if c.SourceDirectory == c.SnapshotRoot || pathContains(c.SourceDirectory, c.SnapshotRoot) || pathContains(c.SnapshotRoot, c.SourceDirectory) {
		return errors.New("Trivy database staging 与 snapshot root 必须隔离")
	}
	return nil
}

func (c Config) SnapshotDirectory() string {
	return filepath.Join(c.SnapshotRoot, "snapshots", c.DatabaseRevision)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
