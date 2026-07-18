package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/cast"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/rs/zerolog/log"
)

// system_settings keys for server-side casting. Same live-toggle
// semantics as Jellyfin/Subsonic: UI-editable unless env-locked.
const (
	castKeyEnabled      = "cast.enabled"
	castKeyBaseURL      = "cast.base_url"
	castKeyDevices      = "cast.devices"
	castKeyAllowedUsers = "cast.allowed_user_ids"
)

var (
	ErrCastAccessDenied     = errors.New("casting is not allowed for this user")
	ErrInvalidCastAllowance = errors.New("invalid casting user allowance")
)

func (a *App) SaveCastSettings(ctx context.Context, enabled bool, baseURL, devices string, allowedUserIDs []int64) error {
	var err error
	baseURL, err = normalizeCastBaseURL(baseURL)
	if err != nil {
		return err
	}
	users, allowedUserIDs, err := a.validateCastAllowedUsers(ctx, allowedUserIDs)
	if err != nil {
		return err
	}

	// Serializing the whole settings transition keeps manager side effects in
	// the same order as persistence without holding the general config lock
	// while receiver sessions are stopped or discovery is restarted.
	a.castSettingsMu.Lock()
	defer a.castSettingsMu.Unlock()
	effective, port, devicesChanged, err := a.persistCastSettings(ctx, enabled, baseURL, devices, allowedUserIDs)
	if err != nil {
		return err
	}

	if a.castMgr != nil {
		effectiveBaseURL, _ := normalizeCastBaseURL(effective.BaseURL.Value)
		a.castMgr.SetMediaOrigin(effectiveBaseURL, port)
	}
	a.setCastAllowedUsers(allowedUserIDs)
	a.stopDisallowedCastSessions(users)
	// Flips take effect immediately: enable starts discovery, disable
	// tears down every active session (receivers see a clean TEARDOWN).
	// A device-list change while running restarts the manager so the
	// resolve loop picks it up — active sessions stop (rare admin action).
	if a.castMgr != nil && (!effective.Enabled.Value || devicesChanged) {
		a.castMgr.Stop()
	}
	if effective.Enabled.Value {
		a.startCast(ctx, effective, port)
	}
	return nil
}

func (a *App) persistCastSettings(ctx context.Context, enabled bool, baseURL, devices string, allowedUserIDs []int64) (config.CastConfig, string, bool, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	cur := a.config.Cast
	if err := errIfEnvLockedChanged(castKeyEnabled, cur.Enabled, enabled); err != nil {
		return config.CastConfig{}, "", false, err
	}
	if err := errIfEnvLockedChanged(castKeyDevices, cur.Devices, devices); err != nil {
		return config.CastConfig{}, "", false, err
	}
	currentBaseURL, err := normalizeCastBaseURL(cur.BaseURL.Value)
	if err != nil {
		return config.CastConfig{}, "", false, fmt.Errorf("current casting media URL is invalid: %w", err)
	}
	baseURLField := cur.BaseURL
	baseURLField.Value = currentBaseURL
	if err := errIfEnvLockedChanged(castKeyBaseURL, baseURLField, baseURL); err != nil {
		return config.CastConfig{}, "", false, err
	}
	if err := persistFieldSetting(a, ctx, castKeyEnabled, cur.Enabled, enabled); err != nil {
		return config.CastConfig{}, "", false, err
	}
	if err := persistFieldSetting(a, ctx, castKeyDevices, cur.Devices, devices); err != nil {
		return config.CastConfig{}, "", false, err
	}
	if err := persistFieldSetting(a, ctx, castKeyBaseURL, cur.BaseURL, baseURL); err != nil {
		return config.CastConfig{}, "", false, err
	}
	allowedJSON, err := json.Marshal(allowedUserIDs)
	if err != nil {
		return config.CastConfig{}, "", false, fmt.Errorf("encoding casting allowance: %w", err)
	}
	if err := a.SetSystemSetting(ctx, castKeyAllowedUsers, allowedJSON); err != nil {
		return config.CastConfig{}, "", false, fmt.Errorf("saving casting allowance: %w", err)
	}
	if a.config.Cast.Enabled.Source != config.SourceEnv {
		a.config.Cast.Enabled = config.Field[bool]{Value: enabled, Source: config.SourceDB}
	}
	if a.config.Cast.Devices.Source != config.SourceEnv {
		a.config.Cast.Devices = config.Field[string]{Value: devices, Source: config.SourceDB}
	}
	if a.config.Cast.BaseURL.Source != config.SourceEnv {
		a.config.Cast.BaseURL = config.Field[string]{Value: baseURL, Source: config.SourceDB}
	}
	effective := a.config.Cast
	devicesChanged := cur.Devices.Value != effective.Devices.Value
	return effective, a.config.Port.Value, devicesChanged, nil
}

// LoadCastFromDB seeds the in-memory snapshot from system_settings at
// boot; env-sourced fields keep their env provenance.
func (a *App) LoadCastFromDB(ctx context.Context) {
	if a.db == nil {
		return
	}
	a.castSettingsMu.Lock()
	defer a.castSettingsMu.Unlock()
	a.configMu.Lock()
	overlayFieldFromDB(a, ctx, &a.config.Cast.Enabled, castKeyEnabled, nil)
	overlayFieldFromDB(a, ctx, &a.config.Cast.BaseURL, castKeyBaseURL, func(v string) bool {
		_, err := normalizeCastBaseURL(v)
		return err == nil
	})
	overlayFieldFromDB(a, ctx, &a.config.Cast.Devices, castKeyDevices, nil)
	a.configMu.Unlock()
	raw, err := a.GetSystemSetting(ctx, castKeyAllowedUsers)
	if errors.Is(err, pgx.ErrNoRows) {
		// Secure default for upgrades and fresh installs: admins retain the
		// implicit recovery path; regular users must be explicitly allowed.
		a.setCastAllowedUsers(nil)
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("cast: load user allowance failed")
		return
	}
	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		log.Error().Err(err).Msg("cast: invalid user allowance setting")
		return
	}
	a.setCastAllowedUsers(ids)
}

func (a *App) CastEnabled() bool {
	a.configMu.RLock()
	enabled := a.config.Cast.Enabled.Value
	a.configMu.RUnlock()
	return enabled
}

func normalizeCastBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid casting media URL: use an absolute http:// or https:// URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("invalid casting media URL: origin must not include credentials, a path, query, or fragment")
	}
	u.Path = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// CastAccessAllowed is the cheap request-path policy check. Admins always
// retain access; regular users must be present in the explicit allowlist.
func (a *App) CastAccessAllowed(userID int64, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	a.castAccessMu.RLock()
	_, ok := a.castAllowedUsers[userID]
	a.castAccessMu.RUnlock()
	return ok
}

func (a *App) requireCastAccess(ctx context.Context, userID int64) (sqlc.User, error) {
	user, err := sqlc.New(a.db).GetUserByID(ctx, userID)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("casting user lookup: %w", err)
	}
	if !a.CastAccessAllowed(user.ID, user.IsAdmin) {
		return sqlc.User{}, ErrCastAccessDenied
	}
	return user, nil
}

// ValidateCastMediaAccess is the second gate on receiver-pull URLs. Tokens are
// user-scoped, so disabling casting, deleting the user, or revoking their cast
// allowance prevents a receiver from opening a new HTTP stream even while the
// signed URL itself has time remaining.
func (a *App) ValidateCastMediaAccess(ctx context.Context, userID int64) error {
	if !a.CastEnabled() {
		return ErrCastAccessDenied
	}
	_, err := a.requireCastAccess(ctx, userID)
	return err
}

func (a *App) setCastAllowedUsers(ids []int64) {
	next := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			next[id] = struct{}{}
		}
	}
	a.castAccessMu.Lock()
	a.castAllowedUsers = next
	a.castAccessMu.Unlock()
}

func (a *App) castAllowedUserIDs() []int64 {
	a.castAccessMu.RLock()
	ids := make([]int64, 0, len(a.castAllowedUsers))
	for id := range a.castAllowedUsers {
		ids = append(ids, id)
	}
	a.castAccessMu.RUnlock()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (a *App) validateCastAllowedUsers(ctx context.Context, ids []int64) ([]sqlc.User, []int64, error) {
	users, err := a.ListUsers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing users for casting allowance: %w", err)
	}
	known := make(map[int64]struct{}, len(users))
	for _, user := range users {
		known[user.ID] = struct{}{}
	}
	unique := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, nil, fmt.Errorf("%w: invalid user id %d", ErrInvalidCastAllowance, id)
		}
		if _, ok := known[id]; !ok {
			return nil, nil, fmt.Errorf("%w: user %d does not exist", ErrInvalidCastAllowance, id)
		}
		unique[id] = struct{}{}
	}
	normalized := make([]int64, 0, len(unique))
	for id := range unique {
		normalized = append(normalized, id)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return users, normalized, nil
}

// stopDisallowedCastSessions applies revocations immediately. Otherwise an
// already-running stream would keep playing while its former owner could no
// longer see or stop it. Admins are included in effective regardless of the
// explicit allowlist.
func (a *App) stopDisallowedCastSessions(users []sqlc.User) {
	if a.castMgr == nil {
		return
	}
	effective := make(map[int64]struct{}, len(users))
	for _, user := range users {
		if a.CastAccessAllowed(user.ID, user.IsAdmin) {
			effective[user.ID] = struct{}{}
		}
	}
	for _, snap := range a.castMgr.Sessions() {
		if _, ok := effective[snap.UserID]; ok {
			continue
		}
		if session, ok := a.castMgr.Session(snap.ID); ok {
			_ = session.Stop()
		}
	}
}

func (a *App) stopCastSessionsForUser(userID int64) {
	if a.castMgr == nil {
		return
	}
	for _, snap := range a.castMgr.SessionsForUser(userID) {
		if session, ok := a.castMgr.Session(snap.ID); ok {
			_ = session.Stop()
		}
	}
}

// StartCast launches discovery when casting is enabled. Idempotent —
// called from serve at boot and again on settings flips.
func (a *App) StartCast(ctx context.Context) {
	a.castSettingsMu.Lock()
	defer a.castSettingsMu.Unlock()

	cfg := a.ConfigSnapshot()
	if cfg == nil {
		return
	}
	a.startCast(ctx, cfg.Cast, cfg.Port.Value)
}

// startCast applies a captured effective config. SaveCastSettings invokes it
// while holding castSettingsMu, whereas the public StartCast takes an
// immutable snapshot first; neither path recursively acquires the config lock.
func (a *App) startCast(ctx context.Context, cfg config.CastConfig, port string) {
	if a.castMgr == nil || !cfg.Enabled.Value {
		return
	}
	a.castMgr.SetStaticDevices(splitCastDevices(cfg.Devices.Value))
	baseURL, err := normalizeCastBaseURL(cfg.BaseURL.Value)
	if err != nil {
		log.Error().Err(err).Str("value", cfg.BaseURL.Value).Msg("cast: invalid receiver media URL; falling back to automatic LAN address")
		baseURL = ""
	}
	a.castMgr.SetMediaOrigin(baseURL, port)
	if err := a.castMgr.Start(a.lifetimeCtx); err != nil {
		// Extraction/spawn problems shouldn't kill the server; devices
		// simply won't appear and the API reports the empty list.
		log.Error().Err(err).Msg("cast: manager start failed")
	}
}

// splitCastDevices parses the HEYA_CAST_DEVICES comma list into clean
// addresses (IP or ip:port), dropping empties from trailing commas.
func splitCastDevices(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if addr := strings.TrimSpace(part); addr != "" {
			out = append(out, addr)
		}
	}
	return out
}

// Cast exposes the manager for handlers/CLI. Nil when the App was built
// without a data dir (spec-dump / humatest fixtures).
func (a *App) Cast() *cast.Manager { return a.castMgr }

// CastConfigView is the Settings payload: values + provenance so the UI
// can grey out env-locked fields.
type CastConfigView struct {
	Enabled        bool    `json:"enabled"`
	EnabledSource  string  `json:"enabled_source"`
	BaseURL        string  `json:"base_url"`
	BaseURLSource  string  `json:"base_url_source"`
	Devices        string  `json:"devices"`
	DevicesSource  string  `json:"devices_source"`
	AllowedUserIDs []int64 `json:"allowed_user_ids"`
}

func (a *App) CastConfig() CastConfigView {
	a.castSettingsMu.Lock()
	defer a.castSettingsMu.Unlock()

	cfg := a.ConfigSnapshot()
	if cfg == nil {
		return CastConfigView{AllowedUserIDs: a.castAllowedUserIDs()}
	}
	cur := cfg.Cast
	baseURL, _ := normalizeCastBaseURL(cur.BaseURL.Value)
	return CastConfigView{
		Enabled:        cur.Enabled.Value,
		EnabledSource:  string(cur.Enabled.Source),
		BaseURL:        baseURL,
		BaseURLSource:  string(cur.BaseURL.Source),
		Devices:        cur.Devices.Value,
		DevicesSource:  string(cur.Devices.Source),
		AllowedUserIDs: a.castAllowedUserIDs(),
	}
}

// CastInterface is one of the server's own network legs — the debug page
// renders these against discovered devices so a subnet mismatch (the #1
// "no devices" cause: containers, VLANs) is visible at a glance.
type CastInterface struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

type CastNetworkStatus struct {
	Enabled    bool                      `json:"enabled"`
	Running    bool                      `json:"running"`
	Interfaces []CastInterface           `json:"interfaces"`
	Devices    []cast.Device             `json:"devices"`
	Static     []cast.StaticTargetStatus `json:"static"`
	Sessions   []cast.SessionSnapshot    `json:"sessions"`
}

// CastStatus assembles the Settings → Casting diagnostics view.
func (a *App) CastStatus() CastNetworkStatus {
	st := CastNetworkStatus{
		Enabled:    a.CastEnabled(),
		Interfaces: castLocalInterfaces(),
		Devices:    []cast.Device{},
		Static:     []cast.StaticTargetStatus{},
		Sessions:   []cast.SessionSnapshot{},
	}
	if a.castMgr != nil {
		st.Running = a.castMgr.Running()
		st.Devices = a.castMgr.Devices()
		st.Static = a.castMgr.StaticStatuses()
		st.Sessions = a.castMgr.Sessions()
	}
	return st
}

// castLocalInterfaces lists the server's up, non-loopback IPv4 legs.
// mDNS discovery can only hear receivers sharing an L2 with one of these.
func castLocalInterfaces() []CastInterface {
	out := []CastInterface{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipn, ok := addr.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			out = append(out, CastInterface{Name: iface.Name, Addr: ipn.String()})
		}
	}
	return out
}

// CastPlayTrack resolves a track to its primary file and starts (or
// retargets) a session on the device. The primary (highest
// quality_score) file feeds the PCM decoder — the receiver gets full
// quality regardless of what browsers would direct-play.
func (a *App) CastPlayTrack(ctx context.Context, userID int64, deviceID string, trackID int64, volume, startSeconds int) (cast.SessionSnapshot, error) {
	if a.castMgr == nil {
		return cast.SessionSnapshot{}, fmt.Errorf("casting unavailable")
	}
	if !a.CastEnabled() {
		return cast.SessionSnapshot{}, fmt.Errorf("casting is disabled")
	}
	user, err := a.requireCastAccess(ctx, userID)
	if err != nil {
		return cast.SessionSnapshot{}, err
	}
	dev, ok := a.castMgr.Device(deviceID)
	if !ok {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: unknown device %q", deviceID)
	}
	track, err := a.castTrackInfo(ctx, trackID, dev.Provider)
	if err != nil {
		return cast.SessionSnapshot{}, err
	}
	// Mid-track handoff: a client transferring local playback passes its
	// position. Same clamp as Session.Seek — never start past the end.
	if startSeconds > 0 {
		if track.Duration > 0 && startSeconds >= track.Duration {
			startSeconds = track.Duration - 1
		}
		track.StartAt = startSeconds
	}
	// Note: the request ctx bounds only the DB reads above. The session
	// itself runs on the cast manager's lifetime.
	// Recheck the in-memory policy after the file lookup so a concurrent
	// admin revocation cannot race a slow request into starting a new stream.
	if !a.CastAccessAllowed(user.ID, user.IsAdmin) {
		return cast.SessionSnapshot{}, ErrCastAccessDenied
	}
	s, err := a.castMgr.Play(deviceID, userID, track, volume)
	if err != nil {
		return cast.SessionSnapshot{}, err
	}
	return s.Snapshot(), nil
}

// CastPlayVideo resolves a library file and starts Chromecast playback. A
// conservative MP4/H.264/AAC source is range-served directly; everything else
// uses Heya's existing HLS pipeline with a safe H.264/AAC fallback. The
// receiver URL is scoped to this file's cast-media subtree by Manager.Play.
func (a *App) CastPlayVideo(ctx context.Context, userID int64, deviceID, fileRef, entityType string, entityID int64, title string, audioTrack int, subtitleTrack *int, quality string, volume, startSeconds int, startPaused bool) (cast.SessionSnapshot, error) {
	if a.castMgr == nil {
		return cast.SessionSnapshot{}, fmt.Errorf("casting unavailable")
	}
	if !a.CastEnabled() {
		return cast.SessionSnapshot{}, fmt.Errorf("casting is disabled")
	}
	user, err := a.requireCastAccess(ctx, userID)
	if err != nil {
		return cast.SessionSnapshot{}, err
	}
	dev, ok := a.castMgr.Device(deviceID)
	if !ok {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: unknown device %q", deviceID)
	}
	if dev.Provider != "chromecast" || !slices.Contains(dev.Capabilities, "video") {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: %s does not support video playback", dev.Name)
	}
	if audioTrack < 0 {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: invalid video audio track")
	}
	if entityType == "" {
		entityType = "movie"
	}
	if entityType != "movie" && entityType != "episode" {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: invalid video entity type %q", entityType)
	}

	file, err := a.GetLibraryFileByRef(ctx, fileRef)
	if err != nil || file.DeletedAt.Valid {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: video file not found")
	}
	file, err = a.EnsureFileProbed(ctx, file.ID)
	if err != nil {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: probing video file: %w", err)
	}
	var mediaInfo mediaprobe.MediaInfo
	if len(file.MediaInfo) > 0 {
		_ = json.Unmarshal(file.MediaInfo, &mediaInfo)
	}
	audioStreams := make([]mediaprobe.StreamInfo, 0)
	subtitleStreams := make([]mediaprobe.StreamInfo, 0)
	for _, stream := range mediaInfo.Streams {
		switch stream.CodecType {
		case "audio":
			audioStreams = append(audioStreams, stream)
		case "subtitle":
			subtitleStreams = append(subtitleStreams, stream)
		}
	}
	if audioTrack > 0 && audioTrack >= len(audioStreams) {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: video audio track %d does not exist", audioTrack)
	}
	var selectedSubtitle *mediaprobe.StreamInfo
	if subtitleTrack != nil {
		if *subtitleTrack < 0 || *subtitleTrack >= len(subtitleStreams) {
			return cast.SessionSnapshot{}, fmt.Errorf("cast: video subtitle track %d does not exist", *subtitleTrack)
		}
		selectedSubtitle = &subtitleStreams[*subtitleTrack]
		if transcoder.SubtitleDeliveryFor(selectedSubtitle.CodecName) != transcoder.SubDeliveryExternal {
			return cast.SessionSnapshot{}, fmt.Errorf("cast: subtitle track %d requires burn-in and cannot be sent to the Default Media Receiver", *subtitleTrack)
		}
	}
	if entityID <= 0 && file.MediaItemID.Valid {
		entityID = file.MediaItemID.Int64
	}
	if entityID <= 0 {
		return cast.SessionSnapshot{}, fmt.Errorf("cast: video has no playback entity")
	}
	if strings.TrimSpace(title) == "" && file.MediaItemID.Valid {
		if item, itemErr := a.GetMediaItem(ctx, fmt.Sprint(file.MediaItemID.Int64)); itemErr == nil {
			title = item.Title
		}
	}
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path))
	}

	info := cast.TrackInfo{
		FileID:      file.PublicID.String(),
		MediaItemID: file.MediaItemID.Int64,
		EntityType:  entityType,
		EntityID:    entityID,
		Path:        file.Path,
		MediaKind:   "video",
		Title:       strings.TrimSpace(title),
		Duration:    int(mediaInfo.Duration),
		AudioTrack:  audioTrack,
		Quality:     "auto",
		StartPaused: startPaused,
	}
	root := fmt.Sprintf("/api/cast/media/video/%s", info.FileID)
	if quality != "" && quality != "auto" {
		if _, exists := transcoder.GetProfile(quality); exists {
			info.Quality = quality
		}
	}
	if selectedSubtitle != nil {
		name := strings.TrimSpace(selectedSubtitle.Tags["title"])
		if name == "" {
			name = strings.ToUpper(strings.TrimSpace(selectedSubtitle.Tags["language"]))
		}
		info.TextTrack = &cast.TextTrackInfo{
			SelectionIndex: *subtitleTrack,
			StreamIndex:    selectedSubtitle.Index,
			TrackID:        1,
			Name:           name,
			Language:       strings.TrimSpace(selectedSubtitle.Tags["language"]),
			PullPath:       fmt.Sprintf("%s/subtitles/%d", root, selectedSubtitle.Index),
		}
		info.PullScopePath = root
	}
	if info.Quality == "auto" && castVideoCanDirect(mediaInfo, file.Path, audioTrack) {
		info.PullPath = root
		info.ContentType = "video/mp4"
	} else {
		if a.TranscoderSessions() == nil {
			return cast.SessionSnapshot{}, fmt.Errorf("cast: this video needs HLS delivery but transcoding is unavailable")
		}
		q := url.Values{}
		q.Set("sid", "cast-"+uuid.NewString())
		if audioTrack > 0 {
			q.Set("audio", fmt.Sprint(audioTrack))
		}
		if info.Quality != "auto" {
			q.Set("quality", info.Quality)
		}
		info.PullPath = root + "/hls/master.m3u8"
		info.PullScopePath = root
		info.PullQuery = q.Encode()
		info.ContentType = "application/vnd.apple.mpegurl"
	}
	if startSeconds > 0 {
		if info.Duration > 0 && startSeconds >= info.Duration {
			startSeconds = info.Duration - 1
		}
		info.StartAt = startSeconds
	}
	if !a.CastAccessAllowed(user.ID, user.IsAdmin) {
		return cast.SessionSnapshot{}, ErrCastAccessDenied
	}
	s, err := a.castMgr.Play(deviceID, userID, info, volume)
	if err != nil {
		return cast.SessionSnapshot{}, err
	}
	return s.Snapshot(), nil
}

// castVideoCanDirect intentionally targets the common denominator across
// Chromecast generations: an SDR, progressive, 8-bit H.264 + AAC MP4 using
// the default audio track. Newer receivers support more, but HLS fallback is
// preferable to a LOAD that succeeds and then fails silently on old hardware.
func castVideoCanDirect(info mediaprobe.MediaInfo, path string, audioTrack int) bool {
	if audioTrack != 0 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".mp4" && ext != ".m4v" {
		return false
	}
	var video, audio *mediaprobe.StreamInfo
	for i := range info.Streams {
		s := &info.Streams[i]
		if s.CodecType == "video" && video == nil {
			video = s
		}
		if s.CodecType == "audio" && audio == nil {
			audio = s
		}
	}
	if video == nil || !strings.EqualFold(video.CodecName, "h264") {
		return false
	}
	pixFmt := strings.ToLower(video.PixFmt)
	profile := strings.ToLower(video.Profile)
	if strings.Contains(pixFmt, "10") || strings.Contains(pixFmt, "12") || strings.Contains(profile, "10") {
		return false
	}
	if video.ColorTransfer == "smpte2084" || video.ColorTransfer == "arib-std-b67" {
		return false
	}
	fieldOrder := strings.ToLower(video.FieldOrder)
	if fieldOrder != "" && fieldOrder != "unknown" && fieldOrder != "progressive" {
		return false
	}
	for _, side := range video.SideDataList {
		if side.Rotation != 0 || side.DvProfile > 0 {
			return false
		}
	}
	return audio == nil || strings.EqualFold(audio.CodecName, "aac")
}

func (a *App) castTrackInfo(ctx context.Context, trackID int64, provider string) (cast.TrackInfo, error) {
	detail, err := a.GetMusicTrackDetail(ctx, trackID)
	if err != nil {
		return cast.TrackInfo{}, fmt.Errorf("track %d not found", trackID)
	}
	files := detail.Files
	if len(files) == 0 {
		return cast.TrackInfo{}, fmt.Errorf("track %d has no playable file", trackID)
	}
	lf, err := a.GetLibraryFile(ctx, files[0].LibraryFileID)
	if err != nil {
		return cast.TrackInfo{}, fmt.Errorf("track %d: library file missing", trackID)
	}
	info := cast.TrackInfo{
		TrackID:   detail.ID,
		Path:      lf.Path,
		MediaKind: "audio",
		Title:     detail.Title,
		Artist:    detail.ArtistName,
		Album:     detail.AlbumTitle,
		Duration:  int(detail.Duration),
	}
	if provider == "chromecast" {
		info.PullPath = fmt.Sprintf("/api/cast/media/music/%d", detail.ID)
		info.PullQuery = "supports_flac_native=1&supports_mp3=1&supports_aac_audio=1&supports_ogg_vorbis=1&supports_opus_audio=1&supports_wav_pcm=1"
		for _, file := range files {
			if transcoder.CanPlayDirect(file.Format, chromecastAudioCaps) {
				info.ContentType = castAudioContentType(file.Format)
				return info, nil
			}
		}
		if a.AudioSessions() == nil {
			return cast.TrackInfo{}, fmt.Errorf("track %d has no Chromecast-compatible source and cannot be transcoded", trackID)
		}
		info.ContentType = "audio/mp4"
	}
	return info, nil
}

var chromecastAudioCaps = transcoder.AudioCaps{
	FLAC: true, MP3: true, AAC: true, Vorbis: true, Opus: true, WavPCM: true,
}

func castAudioContentType(format string) string {
	switch strings.ToLower(format) {
	case "flac":
		return "audio/flac"
	case "mp3":
		return "audio/mpeg"
	case "m4a", "aac":
		return "audio/mp4"
	case "ogg", "oga":
		return "audio/ogg"
	case "opus":
		return "audio/ogg; codecs=opus"
	case "wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}

// castPlaybackSink records cast listens through the same dispatch the
// HTTP playback endpoint uses, with a distinguishing source label.
func (a *App) castPlaybackSink(ctx context.Context, userID int64, item cast.TrackInfo, positionSec, totalSec int, completed bool) {
	entityType := "track"
	entityID := item.TrackID
	if item.MediaKind == "video" {
		entityType = item.EntityType
		entityID = item.EntityID
	}
	_ = a.RecordPlayback(ctx, userID, PlaybackEvent{
		EntityType:      entityType,
		EntityID:        entityID,
		PositionSeconds: int32(positionSec), //nolint:gosec // bounded by track duration
		TotalSeconds:    int32(totalSec),    //nolint:gosec // bounded by track duration
		Completed:       completed,
		Source:          "cast",
	})
}
