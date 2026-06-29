package tailscale

import (
	"context"

	"tailscale.com/ipn/ipnstate"
)

// Manager is the control surface the service layer drives the tailnet node
// through. Handlers call these methods via App.Tailscale() without caring
// which implementation is behind them.
//
//   - *Server is the production implementation: tsnet runs in-process and the
//     same handler serves LAN + tailnet from the single binary.
//   - *RemoteClient is the dev implementation: the real tsnet node lives in the
//     stable `heya dev-proxy` front-door process (so it survives air rebuilds
//     of the backend), and the backend drives it over a localhost control
//     socket. DB-backed enable/disable still flows through the backend exactly
//     as in prod — only the final hop to the node is remote.
type Manager interface {
	Enable(ctx context.Context, cfg Config) error
	Disable() error
	SetFunnel(ctx context.Context, on bool) error
	Logout(ctx context.Context) error
	Status() Status
	RawStatus(ctx context.Context) (*ipnstate.Status, error)
	Close() error
}

var (
	_ Manager = (*Server)(nil)
	_ Manager = (*RemoteClient)(nil)
)
