package tailscale

import (
	"context"

	"tailscale.com/ipn/ipnstate"
)

// Manager is the control surface the service layer drives the tailnet node
// through. Handlers call these methods via App.Tailscale() without caring
// which implementation is behind them. *Server (in-process tsnet, prod only)
// is the sole implementation — under --dev-backend the manager stays nil and
// the API reports Tailscale as unavailable; the dev-proxy is a dumb reverse
// proxy with no tailnet presence.
type Manager interface {
	Enable(ctx context.Context, cfg Config) error
	Disable() error
	SetFunnel(ctx context.Context, on bool) error
	Logout(ctx context.Context) error
	Status() Status
	RawStatus(ctx context.Context) (*ipnstate.Status, error)
	Close() error
}

var _ Manager = (*Server)(nil)
