package plugindev

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Phase string

const (
	PhaseWatching   Phase = "Watching"
	PhaseDebouncing Phase = "Debouncing"
	PhaseBuilding   Phase = "Building"
	PhasePublishing Phase = "Publishing"
	PhaseReady      Phase = "Ready"
	PhaseFailed     Phase = "Failed"
)

type Status struct {
	SchemaVersion int       `json:"schemaVersion"`
	PluginID      string    `json:"pluginId"`
	Target        string    `json:"target"`
	Generation    uint64    `json:"generation"`
	Phase         Phase     `json:"phase"`
	SourceDigest  string    `json:"sourceDigest,omitempty"`
	Version       string    `json:"version,omitempty"`
	PackageFile   string    `json:"packageFile,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type StatusWriter interface {
	Write(Status) error
}

type FileStatusWriter struct{ Path string }

func (w FileStatusWriter) Write(status Status) error {
	if !filepath.IsAbs(w.Path) || status.SchemaVersion != 1 || status.PluginID == "" || status.UpdatedAt.IsZero() {
		return errors.New("插件开发状态写入参数无效")
	}
	raw, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(w.Path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(w.Path), ".status-*.json")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, w.Path)
}
