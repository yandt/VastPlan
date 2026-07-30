package versioningv1

type IsAncestorRequest struct {
	Ancestor   VersionRef `json:"ancestor"`
	Descendant VersionRef `json:"descendant"`
}

type IsAncestorResult struct {
	IsAncestor bool `json:"isAncestor"`
	Distance   int  `json:"distance"`
}

type FindCommonAncestorRequest struct {
	Left  VersionRef `json:"left"`
	Right VersionRef `json:"right"`
}

type FindCommonAncestorResult struct {
	Found         bool        `json:"found"`
	Ancestor      *VersionRef `json:"ancestor,omitempty"`
	LeftDistance  int         `json:"leftDistance"`
	RightDistance int         `json:"rightDistance"`
}
