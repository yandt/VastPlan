package credentialsstate

import (
	"errors"
	"strings"
	"time"
)

const (
	GCStateKey     = "gc.state"
	GCMarkerPrefix = "gc.marker."
	GCStateFormat  = "credentials.chunk-gc-state.v1"
	GCMarkerFormat = "credentials.chunk-gc-marker.v1"

	GCPhaseIdle  = "idle"
	GCPhaseMark  = "mark"
	GCPhaseSweep = "sweep"
)

// ChunkGCState is a small resumable controller checkpoint. It intentionally
// carries no credential name, handle, ciphertext, tenant, or caller identity.
type ChunkGCState struct {
	Format          string     `json:"format"`
	Phase           string     `json:"phase"`
	Cursor          string     `json:"cursor,omitempty"`
	CycleStartedAt  *time.Time `json:"cycleStartedAt,omitempty"`
	LastCompletedAt *time.Time `json:"lastCompletedAt,omitempty"`
	Marked          uint64     `json:"marked"`
	Deleted         uint64     `json:"deleted"`
}

type ChunkGCMarker struct {
	Format          string    `json:"format"`
	Digest          string    `json:"digest"`
	BlobRevision    uint64    `json:"blobRevision"`
	FirstObservedAt time.Time `json:"firstObservedAt"`
}

func NewChunkGCState(startedAt time.Time) ChunkGCState {
	started := startedAt.UTC()
	return ChunkGCState{Format: GCStateFormat, Phase: GCPhaseMark, CycleStartedAt: &started}
}

func ParseChunkGCState(raw []byte) (ChunkGCState, error) {
	var state ChunkGCState
	if err := DecodeStrictJSON(raw, &state); err != nil {
		return state, err
	}
	if state.Format != GCStateFormat {
		return state, errors.New("Credentials chunk GC state format 无效")
	}
	switch state.Phase {
	case GCPhaseIdle:
		if state.Cursor != "" || state.CycleStartedAt != nil || state.LastCompletedAt == nil || state.LastCompletedAt.IsZero() {
			return state, errors.New("Credentials chunk GC idle state 无效")
		}
	case GCPhaseMark:
		if state.Cursor != "" && !strings.HasPrefix(state.Cursor, BlobPrefix) {
			return state, errors.New("Credentials chunk GC mark cursor 无效")
		}
	case GCPhaseSweep:
		if state.Cursor != "" && !strings.HasPrefix(state.Cursor, GCMarkerPrefix) {
			return state, errors.New("Credentials chunk GC sweep cursor 无效")
		}
	default:
		return state, errors.New("Credentials chunk GC phase 无效")
	}
	if state.Phase != GCPhaseIdle && (state.CycleStartedAt == nil || state.CycleStartedAt.IsZero()) {
		return state, errors.New("Credentials chunk GC cycle time 无效")
	}
	return state, nil
}

func NewChunkGCMarker(digest string, revision uint64, observedAt time.Time) (ChunkGCMarker, error) {
	marker := ChunkGCMarker{Format: GCMarkerFormat, Digest: digest, BlobRevision: revision, FirstObservedAt: observedAt.UTC()}
	if err := marker.Validate(); err != nil {
		return ChunkGCMarker{}, err
	}
	return marker, nil
}

func ParseChunkGCMarker(raw []byte) (ChunkGCMarker, error) {
	var marker ChunkGCMarker
	if err := DecodeStrictJSON(raw, &marker); err != nil {
		return marker, err
	}
	return marker, marker.Validate()
}

func (m ChunkGCMarker) Validate() error {
	if m.Format != GCMarkerFormat || !validDigest(m.Digest) || m.BlobRevision == 0 || m.FirstObservedAt.IsZero() {
		return errors.New("Credentials chunk GC marker 无效")
	}
	return nil
}

func GCMarkerKey(digest string) string { return GCMarkerPrefix + digest }
