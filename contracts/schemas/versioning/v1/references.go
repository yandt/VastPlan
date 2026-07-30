package versioningv1

import "time"

type Tag struct {
	Protocol  string     `json:"protocol"`
	Stream    StreamKey  `json:"stream"`
	Name      string     `json:"name"`
	Target    VersionRef `json:"target"`
	ActorID   string     `json:"actorId"`
	CreatedAt time.Time  `json:"createdAt"`
}

type ListHeadsRequest struct {
	Stream StreamKey `json:"stream"`
	Limit  int       `json:"limit"`
	Cursor string    `json:"cursor,omitempty"`
}

type ListHeadsResult struct {
	Heads      []Head `json:"heads"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type CreateHeadRequest struct {
	Stream StreamKey  `json:"stream"`
	Name   string     `json:"name"`
	Target VersionRef `json:"target"`
}

type CreateHeadResult struct {
	Head   Head `json:"head"`
	Reused bool `json:"reused"`
}

type DeleteHeadRequest struct {
	Stream           StreamKey `json:"stream"`
	Name             string    `json:"name"`
	ExpectedRevision uint64    `json:"expectedRevision"`
}

type DeleteHeadResult struct {
	Previous Head `json:"previous"`
}

type CreateTagRequest struct {
	Stream StreamKey  `json:"stream"`
	Name   string     `json:"name"`
	Target VersionRef `json:"target"`
}

type ProviderCreateTagRequest struct {
	Stream  StreamKey  `json:"stream"`
	Name    string     `json:"name"`
	Target  VersionRef `json:"target"`
	ActorID string     `json:"actorId"`
}

type CreateTagResult struct {
	Tag    Tag  `json:"tag"`
	Reused bool `json:"reused"`
}

type GetTagRequest struct {
	Stream StreamKey `json:"stream"`
	Name   string    `json:"name"`
}

type GetTagResult struct {
	Tag Tag `json:"tag"`
}

type ListTagsRequest struct {
	Stream StreamKey `json:"stream"`
	Limit  int       `json:"limit"`
	Cursor string    `json:"cursor,omitempty"`
}

type ListTagsResult struct {
	Tags       []Tag  `json:"tags"`
	NextCursor string `json:"nextCursor,omitempty"`
}
