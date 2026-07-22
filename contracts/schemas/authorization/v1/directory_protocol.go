package authorizationv1

type ExternalIdentity struct {
	Issuer       string `json:"issuer"`
	Subject      string `json:"subject"`
	ClaimsDigest string `json:"claimsDigest"`
}

type DirectoryResolveSubjectRequest struct {
	Identity ExternalIdentity `json:"identity"`
	TenantID string           `json:"tenantId,omitempty"`
}

type DirectoryResolveSubjectResult struct {
	Subject           Subject `json:"subject"`
	DirectoryRevision uint64  `json:"directoryRevision"`
}

type DirectoryResolveGroupsRequest struct {
	Subject Subject `json:"subject"`
	Cursor  string  `json:"cursor,omitempty"`
	Limit   int     `json:"limit"`
}

type ExternalGroup struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Issuer      string `json:"issuer"`
}

type DirectoryResolveGroupsResult struct {
	Groups            []ExternalGroup `json:"groups"`
	DirectoryRevision uint64          `json:"directoryRevision"`
	NextCursor        string          `json:"nextCursor,omitempty"`
}

type DirectoryWatchRevisionRequest struct {
	AfterRevision uint64 `json:"afterRevision"`
	Cursor        string `json:"cursor,omitempty"`
}

type DirectoryWatchRevisionResult struct {
	Revision uint64 `json:"revision"`
	Cursor   string `json:"cursor"`
	Changed  bool   `json:"changed"`
}
