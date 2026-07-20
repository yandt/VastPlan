package commonv1

// ManagedCredentialRef is the non-sensitive, versioned reference shared by
// configuration, material-lease and trusted data-plane contracts. Secret
// material is never represented by this type.
type ManagedCredentialRef struct {
	Handle  string `json:"handle"`
	Scope   string `json:"scope"`
	Owner   string `json:"owner"`
	Purpose string `json:"purpose"`
	Version int64  `json:"version"`
	Name    string `json:"name,omitempty"`
}
