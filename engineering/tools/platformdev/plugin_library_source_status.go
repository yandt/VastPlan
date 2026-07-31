package main

import (
	"path/filepath"

	"cdsoft.com.cn/VastPlan/engineering/internal/pluginlibrarysource"
)

func pluginLibrarySourceStatus(stateRoot string) pluginlibrarysource.State {
	store := pluginlibrarysource.FileStateStore{Path: filepath.Join(stateRoot, "state", "plugin-library-source", "state.json")}
	state, _, err := store.Load()
	if err != nil {
		return pluginlibrarysource.State{SchemaVersion: 1, Sources: map[string]pluginlibrarysource.SourceState{}}
	}
	return state
}
