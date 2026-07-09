package cast

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// Tuning constants — every one of these encodes a live-tested finding
// from docs/casting-research.md; change with care.
const (
	// airplayLatencyMS MUST always be passed: omitting --latency leaves
	// cliap2's commence gate a 0ms-wide window against a 10ms poll grid
	// (signed/unsigned bug) and playback silently never starts on
	// roughly half of runs. 100 is the live-validated value (+250ms DAC
	// floor added internally by the binary).
	airplayLatencyMS = 100

	// airplayNTPLeadSeconds is how far in the future playback is
	// scheduled: covers ~2.5s session establishment + ~4.5s of stdin
	// pre-roll priming. 4s trims audio; 7s is validated clean.
	airplayNTPLeadSeconds = 7

	// airplayCommenceTimeout bounds spawn → event_play_start. Failing
	// transports must die well before the receiver's own ~31s idle-RTSP
	// timeout so a retry gets a clean slate.
	airplayCommenceTimeout = 15 * time.Second

	// stderrBufMax: cliap2 lines are short, but a child-pipe Scanner
	// without an explicit buffer deadlocks the subprocess when a line
	// exceeds the 64KiB default cap.
	stderrBufMax = 256 * 1024
)

// airplayTransport supervises one cliap2 process streaming one track to
// one AirPlay device. Audio arrives on the process's stdin from a
// pcmFeeder; control + metadata go through a command FIFO; state comes
// exclusively from stderr line classification.
type airplayTransport struct {
	dev     Device
	binPath string

	mu       sync.Mutex
	cmd      *exec.Cmd
	feeder   *pcmFeeder
	runDir   string // temp dir holding the command FIFO
	fifoPath string

	playing  bool // event_play_start observed
	ended    bool // end of stream reached observed
	stopping bool // Stop() requested — suppress failure noise

	events   chan TransportEvent
	procDone chan struct{}
	track    TrackInfo
	volume   int
}

func newAirplayTransport(dev Device, binPath string) *airplayTransport {
	return &airplayTransport{
		dev:      dev,
		binPath:  binPath,
		events:   make(chan TransportEvent, 16),
		procDone: make(chan struct{}),
	}
}

func (t *airplayTransport) Events() <-chan TransportEvent { return t.events }

// ntpNow asks the binary for the current time as a raw 64-bit NTP
// timestamp (seconds in the high 32 bits). Kept out of Go time math on
// purpose: the value must be in cliap2's own clock domain.
func (t *airplayTransport) ntpNow(ctx context.Context) (uint64, error) {
	out, err := exec.CommandContext(ctx, t.binPath, "--ntp").Output() //nolint:gosec // binPath is our own extracted embed
	if err != nil {
		return 0, fmt.Errorf("cast: cliap2 --ntp: %w", err)
	}
	ntp, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cast: cliap2 --ntp output %q: %w", strings.TrimSpace(string(out)), err)
	}
	return ntp, nil
}

// txtArg renders the device's mDNS TXT record the way cliap2 expects:
// one argument of space-separated, double-quoted k=v pairs, verbatim.
func txtArg(txt []string) string {
	quoted := make([]string, 0, len(txt))
	for _, kv := range txt {
		quoted = append(quoted, `"`+kv+`"`)
	}
	return strings.Join(quoted, " ")
}

func (t *airplayTransport) Start(ctx context.Context, track TrackInfo, volume int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cmd != nil {
		return fmt.Errorf("cast: transport already started")
	}
	t.track = track
	t.volume = clampVolume(volume)

	ntp, err := t.ntpNow(ctx)
	if err != nil {
		return err
	}
	ntpStart := ntp + uint64(airplayNTPLeadSeconds)<<32

	runDir, err := os.MkdirTemp("", "heya-cast-*")
	if err != nil {
		return err
	}
	t.runDir = runDir
	t.fifoPath = filepath.Join(runDir, "cmd") // cliap2 mkfifos it on start

	feeder, err := newPCMFeeder(ctx, track)
	if err != nil {
		_ = os.RemoveAll(runDir)
		return err
	}
	t.feeder = feeder

	//nolint:gosec // binPath is our own extracted embed; device fields come from mDNS discovery
	cmd := exec.CommandContext(ctx, t.binPath,
		"--name", t.dev.Name,
		"--hostname", t.dev.Host,
		"--address", t.dev.Addr,
		"--port", strconv.Itoa(t.dev.Port),
		"--txt", txtArg(t.dev.TXT),
		"--ntpstart", strconv.FormatUint(ntpStart, 10),
		"--volume", strconv.Itoa(t.volume),
		"--latency", strconv.Itoa(airplayLatencyMS),
		"--loglevel", "4", // DEBUG: the state markers are DEBUG-level lines
		"--command_pipe", t.fifoPath,
	)
	cmd.Stdin = feeder.out
	// SIGTERM on ctx cancel, never SIGKILL: cliap2 must send TEARDOWN or
	// the receiver holds ghost session state.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second

	stderr, err := cmd.StderrPipe()
	if err != nil {
		feeder.stop()
		_ = os.RemoveAll(runDir)
		return err
	}

	if err := feeder.start(); err != nil {
		_ = os.RemoveAll(runDir)
		return fmt.Errorf("cast: ffmpeg spawn: %w", err)
	}
	if err := cmd.Start(); err != nil {
		feeder.stop()
		_ = os.RemoveAll(runDir)
		return fmt.Errorf("cast: cliap2 spawn: %w", err)
	}
	t.cmd = cmd

	go t.readStderr(stderr)
	go t.waitProc()
	go t.commenceWatchdog()
	return nil
}

func (t *airplayTransport) readStderr(r interface{ Read([]byte) (int, error) }) {
	// HEYA_CAST_STDERR_DIR: dump each transport's raw cliap2 stderr to a
	// file for debugging (the classifier only surfaces state edges).
	var dump *os.File
	if dir := os.Getenv("HEYA_CAST_STDERR_DIR"); dir != "" {
		if f, err := os.CreateTemp(dir, "cliap2-*.log"); err == nil {
			dump = f
			defer func() { _ = dump.Close() }()
		}
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), stderrBufMax)
	for sc.Scan() {
		line := sc.Text()
		if dump != nil {
			_, _ = dump.WriteString(line + "\n")
		}
		switch classifyStderrLine(line) {
		case evConnected:
			t.emit(TransportEvent{Kind: TransportConnected})
		case evPlayStart:
			t.mu.Lock()
			t.playing = true
			t.mu.Unlock()
			// FIFO writes are gated on this exact moment: pre-roll
			// writes wedge cliap2's session thread.
			t.sendMetadata()
			t.emit(TransportEvent{Kind: TransportPlaying})
		case evPaused:
			t.emit(TransportEvent{Kind: TransportPaused})
		case evResumed:
			t.emit(TransportEvent{Kind: TransportResumed})
		case evEndOfStream:
			t.mu.Lock()
			t.ended = true
			t.mu.Unlock()
		case evRTSPClosed, evDeviceFailed:
			t.mu.Lock()
			benign := t.ended || t.stopping
			t.mu.Unlock()
			if !benign {
				log.Warn().Str("device", t.dev.Name).Str("line", line).Msg("cast: airplay device dropped session")
			}
		case evNTPTooSoon:
			log.Warn().Str("device", t.dev.Name).Msg("cast: ntpstart lead too tight, initial audio trimmed")
		case evAuthFailed:
			log.Error().Str("device", t.dev.Name).Str("line", line).Msg("cast: airplay auth failure")
		}
	}
}

// commenceWatchdog enforces the "no event_play_start = dead transport"
// rule from the research doc. The receiver would otherwise sit on an
// idle RTSP session until its own ~31s timeout.
func (t *airplayTransport) commenceWatchdog() {
	select {
	case <-t.procDone:
		return
	case <-time.After(airplayCommenceTimeout):
	}
	t.mu.Lock()
	commenced := t.playing
	stopping := t.stopping
	t.mu.Unlock()
	if commenced || stopping {
		return
	}
	log.Warn().Str("device", t.dev.Name).Dur("timeout", airplayCommenceTimeout).
		Msg("cast: playback never commenced, terminating transport")
	t.terminate()
}

func (t *airplayTransport) waitProc() {
	err := t.cmd.Wait()
	close(t.procDone)
	t.mu.Lock()
	t.feeder.stop()
	ended, stopping := t.ended, t.stopping
	runDir := t.runDir
	t.mu.Unlock()
	_ = os.RemoveAll(runDir)

	switch {
	case ended:
		t.emit(TransportEvent{Kind: TransportEnded})
	case stopping:
		// deliberate Stop(); session already knows
	default:
		if err == nil {
			err = fmt.Errorf("cliap2 exited before playback completed")
		}
		t.emit(TransportEvent{Kind: TransportFailed, Err: err})
	}
	close(t.events)
}

func (t *airplayTransport) emit(ev TransportEvent) {
	select {
	case t.events <- ev:
	default:
		log.Warn().Str("device", t.dev.Name).Str("event", string(ev.Kind)).Msg("cast: dropping transport event, consumer stalled")
	}
}

// fifoWrite delivers newline-delimited KEY=value items to cliap2's
// command pipe. Opening a FIFO write-side blocks until the reader end
// exists, so the whole operation is bounded by a deadline goroutine.
func (t *airplayTransport) fifoWrite(items ...string) error {
	t.mu.Lock()
	ok := t.playing && !t.stopping
	path := t.fifoPath
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("cast: transport not in a state that accepts commands")
	}
	done := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // path is our own tempdir FIFO
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = f.Close() }()
		_, err = f.WriteString(strings.Join(items, "\n") + "\n")
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		return fmt.Errorf("cast: command pipe write timed out")
	}
}

func (t *airplayTransport) sendMetadata() {
	tr := t.track
	items := []string{
		"TITLE=" + sanitizeFIFOValue(tr.Title),
		"ARTIST=" + sanitizeFIFOValue(tr.Artist),
		"ALBUM=" + sanitizeFIFOValue(tr.Album),
	}
	if tr.Duration > 0 {
		items = append(items, "DURATION="+strconv.Itoa(tr.Duration))
	}
	if tr.StartAt > 0 {
		items = append(items, "PROGRESS="+strconv.Itoa(tr.StartAt))
	}
	items = append(items, "ACTION=SENDMETA")
	if err := t.fifoWrite(items...); err != nil {
		log.Warn().Err(err).Str("device", t.dev.Name).Msg("cast: metadata push failed")
	}
}

// sanitizeFIFOValue strips newlines — the pipe protocol is
// line-delimited and a crafted tag must not inject commands.
func sanitizeFIFOValue(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "\r", " ")
}

func (t *airplayTransport) Pause() error  { return t.fifoWrite("ACTION=PAUSE") }
func (t *airplayTransport) Resume() error { return t.fifoWrite("ACTION=PLAY") }

func (t *airplayTransport) SetVolume(level int) error {
	level = clampVolume(level)
	t.mu.Lock()
	t.volume = level
	t.mu.Unlock()
	return t.fifoWrite("VOLUME=" + strconv.Itoa(level))
}

// Stop tears down gracefully: ACTION=STOP lets cliap2 send TEARDOWN and
// exit on its own; SIGTERM is the escalation, SIGKILL only via WaitDelay.
func (t *airplayTransport) Stop() error {
	t.mu.Lock()
	if t.cmd == nil || t.stopping {
		t.mu.Unlock()
		return nil
	}
	t.stopping = true
	playing := t.playing
	t.mu.Unlock()

	if playing {
		done := make(chan error, 1)
		go func() {
			f, err := os.OpenFile(t.fifoPath, os.O_WRONLY, 0)
			if err != nil {
				done <- err
				return
			}
			defer func() { _ = f.Close() }()
			_, err = f.WriteString("ACTION=STOP\n")
			done <- err
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		select {
		case <-t.procDone:
			return nil
		case <-time.After(4 * time.Second):
		}
	}
	t.terminate()
	return nil
}

func (t *airplayTransport) terminate() {
	t.mu.Lock()
	cmd := t.cmd
	t.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-t.procDone:
	case <-time.After(4 * time.Second):
		_ = cmd.Process.Kill()
	}
}

func clampVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
