package versioningv1

import "encoding/json"

type CompareVersionsRequest struct {
	Left  VersionRef `json:"left"`
	Right VersionRef `json:"right"`
}

type JSONPatchOperation struct {
	Operation string          `json:"op"`
	Path      string          `json:"path"`
	Value     json.RawMessage `json:"value,omitempty"`
}

type ChangeSummary struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Replaced int `json:"replaced"`
	Total    int `json:"total"`
}

type CompareVersionsResult struct {
	Left    VersionRef           `json:"left"`
	Right   VersionRef           `json:"right"`
	Patch   []JSONPatchOperation `json:"patch"`
	Summary ChangeSummary        `json:"summary"`
}
