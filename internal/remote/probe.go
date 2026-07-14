package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// probeClient talks to the heya.media connectivity-check service (spec:
// POST /v1/check + GET /v1/ip). All requests are pinned to IPv4 — the UPnP
// mapping is v4, so the v4 path is the one whose reachability matters; a
// dual-stack host would otherwise be observed at its v6 address.
type probeClient struct {
	base string
	http *http.Client
}

func newProbeClient(baseURL string) *probeClient {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &probeClient{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			// The service-side probe budget is 10s; leave headroom on top.
			Timeout: 25 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.DialContext(ctx, "tcp4", addr)
				},
				MaxIdleConns:        4,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// observedIP asks the check service what IP this server egresses from.
// Works before any port mapping exists; used for display + CGNAT detection.
func (p *probeClient) observedIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/v1/ip", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Heya/remote-access")
	resp, err := p.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck // defer-close on response body
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check service returned %d", resp.StatusCode)
	}
	var body struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.IP, nil
}

// check runs the outside-in reachability probe. Never returns an error:
// service-side failures land in the result's Error field, client-side
// failures (service unreachable, not deployed yet) set Unavailable so the
// caller can distinguish "unproven" from "proven unreachable".
func (p *probeClient) check(ctx context.Context, port int, challenge string) CheckResult {
	payload, _ := json.Marshal(map[string]any{
		"port":      port,
		"challenge": challenge,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/v1/check", bytes.NewReader(payload))
	if err != nil {
		return CheckResult{Unavailable: true, Error: &CheckError{Code: "client_error", Detail: err.Error()}}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Heya/remote-access")

	resp, err := p.http.Do(req)
	if err != nil {
		return CheckResult{Unavailable: true, Error: &CheckError{Code: "service_unreachable", Detail: err.Error()}}
	}
	defer resp.Body.Close() //nolint:errcheck // defer-close on response body
	if resp.StatusCode != http.StatusOK {
		return CheckResult{Unavailable: true, Error: &CheckError{
			Code:   "service_error",
			Detail: fmt.Sprintf("check service returned %d", resp.StatusCode),
		}}
	}
	var res CheckResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return CheckResult{Unavailable: true, Error: &CheckError{Code: "bad_response", Detail: err.Error()}}
	}
	return res
}
