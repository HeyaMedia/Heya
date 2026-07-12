package playlistsync

import (
	"context"
	"fmt"
)

type Unsupported struct {
	Name   string
	Reason string
}

func (p Unsupported) Service() string            { return p.Name }
func (p Unsupported) IdentityKind() IdentityKind { return IdentityServiceID }
func (p Unsupported) Capabilities() Capabilities {
	return Capabilities{Reason: p.Reason}
}
func (p Unsupported) List(context.Context) ([]Playlist, error) { return []Playlist{}, nil }
func (p Unsupported) Get(context.Context, string) (Playlist, error) {
	return Playlist{}, fmt.Errorf("%s playlist sync is unavailable: %s", p.Name, p.Reason)
}
func (p Unsupported) Create(context.Context, Playlist) (string, error) {
	return "", fmt.Errorf("%s playlist sync is unavailable: %s", p.Name, p.Reason)
}
func (p Unsupported) Replace(context.Context, string, Playlist) error {
	return fmt.Errorf("%s playlist sync is unavailable: %s", p.Name, p.Reason)
}
