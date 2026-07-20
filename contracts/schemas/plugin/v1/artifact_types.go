package pluginv1

import "encoding/json"

// ArtifactRef 唯一定位一个已发布制品。它是制品生产者与消费者之间的稳定 DTO，
// 不包含任何仓库实现细节。
type ArtifactRef struct {
	PluginID string `json:"pluginId"`
	Version  string `json:"version"`
	Channel  string `json:"channel"`
}

// Artifact 是经 schema 验证的可审计制品元数据。
type Artifact struct {
	SchemaVersion string          `json:"schemaVersion"`
	PluginID      string          `json:"pluginId"`
	Version       string          `json:"version"`
	Channel       string          `json:"channel"`
	SHA256        string          `json:"sha256"`
	Size          int64           `json:"size"`
	Object        string          `json:"object"`
	Manifest      json.RawMessage `json:"manifest"`
}
