package pluginlibrarysource

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type StateStore interface {
	Load() (State, bool, error)
	Save(State) error
}

type FileStateStore struct{ Path string }

func (s FileStateStore) Load() (State, bool, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{SchemaVersion: stateSchemaVersion, Sources: map[string]SourceState{}}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil || state.SchemaVersion != stateSchemaVersion || state.Sources == nil {
		return State{}, false, errors.New("Local Plugin Library 源状态无效")
	}
	return state, true, nil
}

func (s FileStateStore) Save(state State) error {
	if s.Path == "" || state.SchemaVersion != stateSchemaVersion || state.Sources == nil {
		return errors.New("Local Plugin Library 源状态写入参数无效")
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.Path), ".source-state-")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
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
	if err := os.Rename(path, s.Path); err != nil {
		return fmt.Errorf("提交 Local Plugin Library 源状态: %w", err)
	}
	directory, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
