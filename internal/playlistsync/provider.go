// Package playlistsync defines the provider boundary for two-way playlist
// synchronization. Providers translate their native identifiers and payloads
// into this small model; database matching and merge policy stay in service.
package playlistsync

import (
	"context"
	"time"
)

type Capabilities struct {
	Available bool   `json:"available"`
	Read      bool   `json:"read"`
	Write     bool   `json:"write"`
	Reason    string `json:"reason,omitempty"`
}

type IdentityKind string

const (
	IdentityRecordingMBID IdentityKind = "recording_mbid"
	IdentityServiceID     IdentityKind = "service_id"
	IdentityISRC          IdentityKind = "isrc"
)

type Track struct {
	ProviderID string `json:"provider_id"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
}

type Playlist struct {
	ExternalID  string    `json:"external_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	URL         string    `json:"url,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
	Tracks      []Track   `json:"tracks,omitempty"`
}

type Provider interface {
	Service() string
	IdentityKind() IdentityKind
	Capabilities() Capabilities
	List(ctx context.Context) ([]Playlist, error)
	Get(ctx context.Context, externalID string) (Playlist, error)
	Create(ctx context.Context, playlist Playlist) (string, error)
	Replace(ctx context.Context, externalID string, playlist Playlist) error
}

// CollectionProvider is implemented by services which expose provider-owned
// playlist feeds in addition to normal user-owned playlists. These feeds are
// always linked pull-only.
type CollectionProvider interface {
	Collections() []Collection
	ListCollection(ctx context.Context, key string) ([]Playlist, error)
}

type Collection struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MergeTrackIDs performs a three-way set/sequence merge. Deleting a track on
// either side removes a base item; additions from both sides survive. A pure
// reorder on only one side is retained. When both reorder concurrently the
// remote order is used, with local-only additions appended deterministically.
func MergeTrackIDs(base, local, remote []string) []string {
	base = unique(base)
	local = unique(local)
	remote = unique(remote)
	if equal(local, base) {
		return remote
	}
	if equal(remote, base) {
		return local
	}

	baseSet, localSet, remoteSet := set(base), set(local), set(remote)
	keep := map[string]bool{}
	for id := range baseSet {
		keep[id] = localSet[id] && remoteSet[id]
	}
	for id := range localSet {
		if !baseSet[id] {
			keep[id] = true
		}
	}
	for id := range remoteSet {
		if !baseSet[id] {
			keep[id] = true
		}
	}

	out := make([]string, 0, len(keep))
	seen := map[string]bool{}
	appendFrom := func(ids []string) {
		for _, id := range ids {
			if keep[id] && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	appendFrom(remote)
	appendFrom(local)
	return out
}

func unique(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func set(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
