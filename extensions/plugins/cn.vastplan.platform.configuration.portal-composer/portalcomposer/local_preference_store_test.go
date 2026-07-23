package portalcomposer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func openPreferenceStore(path string) (*preferenceStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	value := emptyPreferenceState()
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := decodePreferenceState(raw, &value); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	store := &preferenceStore{state: value, now: time.Now}
	store.persist = func(next preferenceState, expected uint64) (uint64, error) {
		_ = expected
		raw, err := json.Marshal(next)
		if err != nil {
			return 0, err
		}
		temporary, err := os.CreateTemp(filepath.Dir(path), ".portal-preferences-test-*")
		if err != nil {
			return 0, err
		}
		name := temporary.Name()
		defer os.Remove(name)
		if err := temporary.Chmod(0o600); err != nil {
			_ = temporary.Close()
			return 0, err
		}
		if _, err := temporary.Write(raw); err != nil {
			_ = temporary.Close()
			return 0, err
		}
		if err := temporary.Close(); err != nil {
			return 0, err
		}
		if err := os.Rename(name, path); err != nil {
			return 0, err
		}
		return store.revision + 1, nil
	}
	return store, nil
}
