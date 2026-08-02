// Package pluginmarketplace defines the neutral, bounded catalog contract used
// by Marketplace providers. A market is a discovery source, not an artifact
// trust boundary: selected artifacts must still enter the platform repository
// and installation workflow before they can run.
package pluginmarketplace

import "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"

const (
	ProtocolVersion = 1
	Capability      = "platform.artifacts.marketplace"
	ListSourcesOp   = "listSources"
	ListCatalogOp   = "listCatalog"
	HealthOp        = "health"
)

type Source struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	URL      string `json:"url"`
	Priority int    `json:"priority"`
}

type ListSourcesResult struct {
	Version int      `json:"version"`
	Sources []Source `json:"sources"`
}

type CatalogRequest struct {
	Version  int                                   `json:"version"`
	SourceID string                                `json:"sourceId"`
	Query    platformadminapi.ArtifactCatalogQuery `json:"query"`
}

type CatalogPage struct {
	Version int    `json:"version"`
	Source  Source `json:"source"`
	platformadminapi.ArtifactCatalogPage
}

type HealthRequest struct {
	Version  int    `json:"version"`
	SourceID string `json:"sourceId"`
}

type Health struct {
	Version  int    `json:"version"`
	Source   Source `json:"source"`
	Ready    bool   `json:"ready"`
	Revision uint64 `json:"revision,omitempty"`
	Message  string `json:"message,omitempty"`
}
