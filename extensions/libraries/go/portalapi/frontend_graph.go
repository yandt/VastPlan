package portalapi

// FrontendModuleGraph is the browser-safe projection of one verified plugin
// graph. Server graphs remain inside the trusted Node Portal Kernel.
type FrontendModuleGraph struct {
	PluginRef
	Target        string               `json:"target"`
	Entry         string               `json:"entry"`
	Digest        string               `json:"digest"`
	PackageSHA256 string               `json:"packageSha256"`
	Externals     []string             `json:"externals"`
	Nodes         []FrontendModuleNode `json:"nodes"`
	Deferred      bool                 `json:"deferred,omitempty"`
}

type FrontendModuleNode struct {
	Path         string                     `json:"path"`
	URL          string                     `json:"url"`
	SHA256       string                     `json:"sha256"`
	Size         int64                      `json:"size"`
	MediaType    string                     `json:"mediaType"`
	Purpose      string                     `json:"purpose"`
	Dependencies []FrontendModuleDependency `json:"dependencies"`
}

type FrontendModuleDependency struct {
	Specifier string `json:"specifier"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
}
