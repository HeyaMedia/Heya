package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"tailscale.com/ipn/ipnstate"
)

// RemoteClient is the dev-mode Manager. It forwards control calls to a
// `heya dev-proxy` front-door over a localhost unix socket — the front-door
// owns the real tsnet node so it stays up across air rebuilds of the backend.
//
// It also self-heals: the backend re-asserts desired state on every boot, and
// the poll loop re-asserts if it ever finds the node down while it should be
// up (e.g. you restarted just the front-door pane to iterate on proxy/tsnet
// code). Server.Enable is idempotent, so a steady-state re-assert is a no-op.
type RemoteClient struct {
	http     *http.Client
	logger   zerolog.Logger
	onStatus StatusFn

	mu     sync.Mutex
	last   Status
	cancel context.CancelFunc

	// desired is the state the backend last asked for. The poll loop drives
	// the front-door back to it if the node drops out from under us.
	desiredMu  sync.Mutex
	desiredCfg Config
	desiredOn  bool
}

// NewRemoteClient dials the control socket at socketPath. onStatus is wired to
// the same hub emitter prod uses, so the UI gets live tailscale events in dev;
// it fires whenever the polled status changes meaningfully.
func NewRemoteClient(socketPath string, logger zerolog.Logger, onStatus StatusFn) *RemoteClient {
	rc := &RemoteClient{
		logger:   logger,
		onStatus: onStatus,
		// No client-wide timeout: Enable can block up to ~90s on first-time
		// interactive auth. Bounded waits are applied per-call via context
		// where they matter (status polls, the skip-guard).
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc.cancel = cancel
	go rc.poll(ctx)
	return rc
}

func (rc *RemoteClient) Enable(ctx context.Context, cfg Config) error {
	rc.setDesired(cfg, true)

	// The backend re-asserts Enable on every boot (incl. each air restart).
	// If the front-door node is already up in the desired shape, skip the
	// call so it doesn't needlessly rebind its tailnet listeners — that would
	// blip any in-flight tailnet connection on every code save. Query the
	// front-door directly (the cache is empty on a fresh process); bound it so
	// a missing front-door doesn't delay the real Enable.
	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	var cur Status
	err := rc.get(sctx, "/status", &cur)
	cancel()
	if err == nil && cur.Running &&
		cur.HTTPS == cfg.HTTPS && cur.Funnel == cfg.Funnel &&
		(cfg.Hostname == "" || cur.Hostname == cfg.Hostname) {
		rc.update(cur)
		return nil
	}
	return rc.post(ctx, "/enable", cfg)
}

func (rc *RemoteClient) Disable() error {
	rc.setDesiredOn(false)
	return rc.post(context.Background(), "/disable", nil)
}

func (rc *RemoteClient) SetFunnel(ctx context.Context, on bool) error {
	rc.desiredMu.Lock()
	rc.desiredCfg.Funnel = on
	rc.desiredMu.Unlock()
	return rc.post(ctx, "/funnel", map[string]bool{"on": on})
}

func (rc *RemoteClient) Logout(ctx context.Context) error {
	rc.setDesiredOn(false)
	return rc.post(ctx, "/logout", nil)
}

func (rc *RemoteClient) Status() Status {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.last
}

func (rc *RemoteClient) RawStatus(ctx context.Context) (*ipnstate.Status, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var st ipnstate.Status
	if err := rc.get(cctx, "/raw", &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Close stops the status poll loop. It does NOT tear down the front-door's
// node — that process owns the node lifecycle and outlives the backend.
func (rc *RemoteClient) Close() error {
	if rc.cancel != nil {
		rc.cancel()
	}
	return nil
}

func (rc *RemoteClient) setDesired(cfg Config, on bool) {
	rc.desiredMu.Lock()
	rc.desiredCfg = cfg
	rc.desiredOn = on
	rc.desiredMu.Unlock()
}

func (rc *RemoteClient) setDesiredOn(on bool) {
	rc.desiredMu.Lock()
	rc.desiredOn = on
	rc.desiredMu.Unlock()
}

func (rc *RemoteClient) desired() (Config, bool) {
	rc.desiredMu.Lock()
	defer rc.desiredMu.Unlock()
	return rc.desiredCfg, rc.desiredOn
}

func (rc *RemoteClient) poll(ctx context.Context) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			var st Status
			err := rc.get(cctx, "/status", &st)
			cancel()
			if err != nil {
				continue // front-door not up yet, or mid-restart
			}
			rc.update(st)

			// Self-heal: the front-door was restarted (or the node dropped)
			// but the backend still wants it enabled — bring it back. The
			// synchronous post throttles this naturally (no overlapping
			// re-asserts), and a steady-state node makes it a no-op.
			if cfg, on := rc.desired(); on && !st.Running {
				if err := rc.post(ctx, "/enable", cfg); err != nil {
					rc.logger.Debug().Err(err).Msg("tailscale re-assert failed; will retry")
				}
			}
		}
	}
}

// update caches the latest status and fires onStatus only on a meaningful
// change (UpdatedAt churns every tick, so it's excluded from the comparison).
func (rc *RemoteClient) update(st Status) {
	rc.mu.Lock()
	prev := rc.last
	rc.last = st
	rc.mu.Unlock()

	a, b := prev, st
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	if a != b && rc.onStatus != nil {
		rc.onStatus(st)
	}
}

func (rc *RemoteClient) post(ctx context.Context, path string, body any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+path, &buf)
	if err != nil {
		return err
	}
	resp, err := rc.http.Do(req)
	if err != nil {
		return fmt.Errorf("tailscale control unreachable (is `heya dev-proxy` running?): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("tailscale control %s: %s", path, bytes.TrimSpace(msg))
	}
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err == nil {
		rc.update(st)
	}
	return nil
}

func (rc *RemoteClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return err
	}
	resp, err := rc.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("tailscale control %s: %s", path, bytes.TrimSpace(msg))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
