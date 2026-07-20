// Generated from shared/api.openapi.json by @hey-api/openapi-ts.
// Do not edit by hand; run `make gen-api-client`.

export type ClientOptions = {
    baseUrl: `${string}://${string}` | (string & {});
};

export type AiAgentStatus = {
    authenticated: boolean;
    binary_present: boolean;
    provider?: string;
    setup_hint?: string;
};

export type AiChatRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    max_tokens?: number;
    /**
     * full message history; overrides prompt/system
     */
    messages?: Array<Message> | null;
    prompt?: string;
    /**
     * optional system prompt / context
     */
    system?: string;
};

export type AiChatResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    completion_tokens: number;
    content: string;
    duration_ms: number;
    mode: string;
    model?: string;
    prompt_tokens: number;
};

export type AiLocalStatus = {
    /**
     * pinned llama.cpp release
     */
    build: string;
    download_error?: string;
    download_progress?: DownloadProgress;
    download_state: string;
    model_present: boolean;
    running: boolean;
    running_model?: string;
    server_present: boolean;
};

export type AiMusicMixRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Number of tracks (default 30)
     */
    limit?: number;
    /**
     * Narrative description of the desired mix
     */
    query: string;
};

export type AiMusicMixResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    duration_ms: number;
    mode: string;
    model?: string;
    /**
     * Acoustic CLAP searches derived from the brief
     */
    probes: Array<string> | null;
    summary: string;
    title: string;
    tracks: Array<AiMusicMixTrack> | null;
};

export type AiMusicMixTrack = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    distance: number;
    duration: number;
    reason?: string;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type AiRecommendRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    limit?: number;
    query: string;
    type?: string;
};

export type AiRecommendResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    duration_ms: number;
    items: Array<ForYouItem> | null;
    mode: string;
    model?: string;
    /**
     * the model's overall explanation of how it read the ask and why the picks fit
     */
    note?: string;
    /**
     * embedding probes the model searched with
     */
    probes?: Array<string> | null;
};

export type AiSettings = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    api_key: string;
    base_url: string;
    claude_model: string;
    claude_token: string;
    codex_model: string;
    context_size: number;
    local_backend: string;
    local_model: string;
    mode: string;
    model: string;
    provider: string;
};

export type AiSettingsView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * last 4 characters, for recognition only
     */
    api_key_hint?: string;
    api_key_set: boolean;
    base_url: string;
    claude_model: string;
    /**
     * last 4 characters, for recognition only
     */
    claude_token_hint?: string;
    claude_token_set: boolean;
    codex_model: string;
    context_size: number;
    local_backend: string;
    local_model: string;
    mode: string;
    model: string;
    provider: string;
};

export type AiStatusReport = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    agent: AiAgentStatus;
    context_size?: number;
    /**
     * human-readable reason when not ready
     */
    detail?: string;
    local: AiLocalStatus;
    local_model?: string;
    mode: string;
    model?: string;
    provider?: string;
    ready: boolean;
};

export type AccentDerived = {
    accent?: string;
    bright?: string;
    deep?: string;
    ink?: string;
    rgb?: string;
};

export type AcquireItem = {
    media_type: string;
    score: number;
    title: string;
    tmdb_id: string;
};

export type ActiveSessionsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<Session> | null;
};

export type ActivityItem = {
    image_url?: string;
    media_id?: number;
    media_type?: string;
    slug?: string;
    subtitle?: string;
    timestamp: string;
    title: string;
    type: string;
};

export type AddListItemRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    media_item_id: number;
};

export type AddedBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    added: number;
};

export type AdminCreateUserRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    email: string;
    is_admin: boolean;
    password: string;
    username: string;
};

export type AdminResetUserPasswordRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    new_password: string;
};

export type AdminSetLogLevelRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * New zerolog level
     */
    level: 'trace' | 'debug' | 'info' | 'warn' | 'error' | 'fatal' | 'panic' | 'disabled';
};

export type AdminSetUserRoleRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    is_admin: boolean;
};

export type AdminStorageScanRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Scan a single library; omit to scan all
     */
    library_id?: number;
};

export type AdminDbBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    acquire_count: number;
    acquire_duration_ms: number;
    acquired_connections: number;
    active_queries: number;
    blocks_hit: number;
    blocks_read: number;
    buffer_cache_hit_ratio: number;
    canceled_acquire_count: number;
    database_name: string;
    dead_tuples: number;
    deadlocks: number;
    empty_acquire_count: number;
    error?: string;
    idle_connections: number;
    index_scan_ratio: number;
    longest_query_ms: number;
    max_connections: number;
    query_stats_available: boolean;
    query_stats_error?: string;
    rows_deleted: number;
    rows_fetched: number;
    rows_inserted: number;
    rows_returned: number;
    rows_updated: number;
    size_bytes: number;
    temp_bytes: number;
    top_queries: Array<AdminDbQuery> | null;
    top_tables: Array<AdminDbTable> | null;
    total_connections: number;
    transactions_committed: number;
    transactions_rolled_back: number;
    version: string;
    waiting_queries: number;
};

export type AdminDbQuery = {
    average_ms: number;
    calls: number;
    max_ms: number;
    rows: number;
    statement: string;
    total_duration_ms: number;
};

export type AdminDbTable = {
    name: string;
    size_bytes: number;
};

export type AdminDiagnosticFinding = {
    detail: string;
    section: 'runtime' | 'traffic' | 'database' | 'queries' | 'logs';
    title: string;
    tone: 'good' | 'warn' | 'bad';
};

export type AdminDiagnosticsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    database: AdminDbBody;
    findings: Array<AdminDiagnosticFinding> | null;
    generated_at: string;
    http: HttpMetrics;
    http_available: boolean;
    logs: AdminLogSummary;
    queries: QuerySnapshot;
    status: 'healthy' | 'watching' | 'degraded';
    system: AdminSystemBody;
    worker: WorkerRuntimeStatus;
    worker_online: boolean;
};

export type AdminListener = {
    active: boolean;
    address: string;
    description?: string;
    error?: string;
    kind: string;
    name?: string;
    network?: string;
    protocols?: Array<string> | null;
    public: boolean;
    tls: boolean;
};

export type AdminListenersBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    listeners: Array<AdminListener> | null;
    ws_subscribers: number;
};

export type AdminLogLevelBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    available: Array<string> | null;
    /**
     * Level loaded from HEYA_LOG_LEVEL at boot
     */
    boot_level: string;
    level: string;
};

export type AdminLogSummary = {
    buffered: number;
    capacity: number;
    counts: {
        [key: string]: number;
    };
    last_5_minutes: {
        [key: string]: number;
    };
    latest_at?: string;
    recent: Array<Entry> | null;
};

export type AdminNetworkGeneral = {
    bind_address: string;
    hostname: string;
    https_required: boolean;
    interfaces: Array<AdminNetworkInterface> | null;
    internal_subscribers: number;
    lan_ip?: string;
    ws_admin_subscribers: number;
    ws_subscribers: number;
};

export type AdminNetworkInterface = {
    addresses?: Array<string> | null;
    error?: string;
    flags?: Array<string> | null;
    hardware_address?: string;
    mtu: number;
    name: string;
};

export type AdminNetworkStatusBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    general: AdminNetworkGeneral;
    ingress: IngressStatus;
    remote?: RemoteStatus;
    tailscale?: Status;
    updated_at: string;
};

export type AdminSessionView = {
    created_at: string;
    expires_at?: string;
    id: number;
    ip?: string;
    is_admin: boolean;
    kind: string;
    last_seen_at: string;
    name?: string;
    user_agent?: string;
    user_id: number;
    username: string;
};

export type AdminStorageBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    data_dir: string;
    data_dir_volume: AdminStoragePath;
    /**
     * Cached results from the last scan_library_disk run; empty until a scan completes
     */
    library_disk_usage: Array<LibraryDiskUsage> | null;
    library_paths: Array<AdminStoragePath> | null;
    transcode_dir: string;
    transcode_items: number;
    transcode_max_gb: number;
    transcode_used_mb: number;
    transcode_volume: AdminStoragePath;
};

export type AdminStoragePath = {
    error?: string;
    exists: boolean;
    free_bytes?: number;
    is_dir: boolean;
    label: string;
    path: string;
    total_bytes?: number;
    used_bytes?: number;
    used_pct?: number;
};

export type AdminSystemBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    build?: {
        [key: string]: unknown;
    };
    /**
     * Serve process CPU where one fully occupied logical core equals 100 percent
     */
    cpu_percent: number;
    gc_pause_last_ns: number;
    go_version: string;
    goarch: string;
    gomaxprocs: number;
    goos: string;
    goroutines: number;
    heap_alloc_bytes: number;
    heap_inuse_bytes: number;
    /**
     * Whether the host exposes a readable CPU counter
     */
    host_cpu_available: boolean;
    /**
     * cpu_utilization on Linux or load_average_1m on macOS
     */
    host_cpu_metric: string;
    /**
     * Whole-host load as a percentage of logical CPU capacity
     */
    host_cpu_percent: number;
    hostname: string;
    num_cgo_call: number;
    num_cpu: number;
    num_gc: number;
    pid: number;
    stack_bytes: number;
    started_at: string;
    sys_bytes: number;
    uptime_seconds: number;
    ws_subscribers: number;
};

export type AdminUserView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    created_at: string;
    email: string;
    id: number;
    is_admin: boolean;
    username: string;
};

export type AdminWorkersBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    active_jobs: Array<JobRow> | null;
    error?: string;
    generated_at: string;
    online: boolean;
    queue_summary: Array<JobSummaryRow> | null;
    recent_jobs: Array<JobRow> | null;
    status: WorkerRuntimeStatus;
};

export type AiCatalogBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    local_models: Array<LocalModel> | null;
    providers: Array<Provider> | null;
};

export type AiModelsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    models: Array<string> | null;
};

export type AiReadyBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    mode: string;
    ready: boolean;
};

export type Album = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artwork: string;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    true_peak_db: Numeric;
    year: string;
};

export type AlbumArtworkRef = {
    type: string;
    url: string;
};

export type AlbumEdition = {
    barcode?: string;
    country?: string;
    date?: string;
    formats?: Array<string> | null;
    labels?: Array<AlbumEditionLabel> | null;
    link?: string;
    provider: string;
    provider_id?: string;
    status?: string;
    title?: string;
    track_count?: number;
};

export type AlbumEditionLabel = {
    catalog_number?: string;
    name: string;
};

export type AlbumIdsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * List of album IDs to look up
     */
    album_ids: Array<number> | null;
};

export type AlbumRating = {
    scale_max: number;
    system: string;
    value: number;
    votes?: number;
};

export type AlbumReleaseEvent = {
    country?: string;
    date: string;
};

export type AlbumResultsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<SimilarAlbumsRow> | null;
};

export type AmbientBackdropItem = {
    has_backdrop: boolean;
    id: number;
    media_type: string;
    public_id: string;
    slug: string;
    title: string;
};

export type ApiTokenView = {
    created_at: string;
    expires_at?: string;
    id: number;
    last_seen_at: string;
    name: string;
};

export type AppearanceSettings = {
    accent?: string;
    accent_custom?: string;
    accent_custom_derived?: AccentDerived;
    ambient_intensity?: number;
    ambient_mode?: string;
    density?: string;
    font_scale?: string;
    glass?: string;
    hero?: string;
    lighting?: string;
    motion?: string;
    radius?: string;
    scrollbar?: string;
    show_unavailable_recs: boolean;
    theme?: string;
    tinted_captions?: boolean;
    tone_follow?: boolean;
    typeset?: string;
};

export type ApplyAlbumIdentifyRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    provider_id: string;
    provider_name: string;
};

export type ApplyIdentifyRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    provider_id: string;
    provider_name: string;
};

export type ArtifactStatus = {
    name: string;
    present: boolean;
    role: string;
    shared: boolean;
    size: number;
};

export type ArtistIdsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * List of artist IDs to look up
     */
    artist_ids: Array<number> | null;
};

export type ArtistMember = {
    begin_year?: number;
    end_year?: number;
    local_slug?: string;
    mbid?: string;
    name: string;
};

export type ArtistPlayQueueBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListArtistTracksTopPlayedFirstRow> | null;
};

export type ArtistResultsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<SimilarArtistsRow> | null;
};

export type ArtistTopTrackRow = {
    listeners: number;
    local_album_id?: number;
    local_album_slug?: string;
    local_album_title?: string;
    local_album_year?: string;
    local_cover_path?: string;
    local_duration?: number;
    local_track_id?: number;
    mbid?: string;
    playcount: number;
    provider?: string;
    rank: number;
    title: string;
    url?: string;
};

export type ArtistUrl = {
    type: string;
    url: string;
};

export type ArtistView = {
    aliases?: Array<string> | null;
    annotation?: string;
    artist_type?: string;
    begin_date?: string;
    begin_year?: number;
    biography?: string;
    birthplace?: string;
    cover_art_enriched_at?: string;
    deathday?: string;
    disambiguation?: string;
    discography_enriched_at?: string;
    end_date?: string;
    ended?: boolean;
    followers?: number;
    genres?: Array<string> | null;
    groups?: Array<ArtistMember> | null;
    id: number;
    listeners?: number;
    media_item_id: number;
    members?: Array<ArtistMember> | null;
    metadata_sources?: Array<string> | null;
    musicbrainz_id?: string;
    name: string;
    playcount?: number;
    popularity?: number;
    profiles?: {
        [key: string]: string;
    };
    sort_name?: string;
    tags?: Array<string> | null;
    urls?: Array<ArtistUrl> | null;
    wikipedia_links?: {
        [key: string]: string;
    };
};

export type ArtworkBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    results: unknown;
};

export type AudioStream = {
    bit_rate?: string;
    channel_layout?: string;
    channels: number;
    codec: string;
    codec_long: string;
    index: number;
    is_default: boolean;
    language: string;
    sample_rate?: string;
    title?: string;
};

export type AuthBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Session token
     */
    token: string;
    user: UserView;
};

export type AuthSessionView = {
    created_at: string;
    current: boolean;
    expires_at?: string;
    id: number;
    ip?: string;
    last_seen_at: string;
    user_agent?: string;
};

export type BatchRatingsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Map of track_id (as string) → rating 1..10. Tracks the user hasn't rated are omitted entirely.
     */
    ratings: {
        [key: string]: number;
    };
};

export type BrowseBucketArtist = {
    id: number;
    public_id: string;
};

export type CancelBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    cancelled: number;
    status: string;
};

export type Capabilities = {
    available: boolean;
    read: boolean;
    reason?: string;
    write: boolean;
};

export type CastPlayRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Zero-based audio-stream selection for video
     */
    audio_track?: number;
    /**
     * Target device (from /api/cast/devices)
     */
    device_id: string;
    /**
     * Movie media-item ID or TV episode ID for video progress
     */
    entity_id?: number;
    /**
     * Watch-progress entity type for video
     */
    entity_type?: 'movie' | 'episode';
    /**
     * Video library-file reference; mutually exclusive with track_id
     */
    file_id?: string;
    /**
     * Optional HLS quality profile for video; auto uses the source-compatible plan
     */
    quality?: string;
    /**
     * Load video paused; used when changing remote track options while paused
     */
    start_paused?: boolean;
    /**
     * Start position in the media item — lets a client hand off mid-playback
     */
    start_seconds?: number;
    /**
     * Zero-based text-subtitle selection for video; omit for subtitles off
     */
    subtitle_track?: number;
    /**
     * Display title for video playback
     */
    title?: string;
    /**
     * Music track to play; mutually exclusive with file_id
     */
    track_id?: number;
    /**
     * Initial device volume (ignored when retargeting an existing session)
     */
    volume: number;
};

export type CastSeekRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Absolute position in the track
     */
    seconds: number;
};

export type CastVolumeRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Device stream volume
     */
    level: number;
};

export type CastConfigView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    allowed_user_ids: Array<number> | null;
    base_url: string;
    base_url_source: string;
    devices: string;
    devices_source: string;
    enabled: boolean;
    enabled_source: string;
};

export type CastInterface = {
    addr: string;
    name: string;
};

export type CastNetworkStatus = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    devices: Array<Device> | null;
    enabled: boolean;
    interfaces: Array<CastInterface> | null;
    running: boolean;
    sessions: Array<SessionSnapshot> | null;
    static: Array<StaticTargetStatus> | null;
};

export type Category = {
    id: number;
    name: string;
};

export type CertStatus = {
    error?: string;
    expiry?: string;
    issuing: boolean;
    mode: string;
    sans?: Array<string> | null;
};

export type CertificateStatus = {
    error?: string;
    expires_at?: string;
    name: string;
    source: string;
    subject: string;
};

export type ChangePasswordRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Current password (verified before swap)
     */
    current_password: string;
    /**
     * New password — minimum 8 chars
     */
    new_password: string;
};

export type CheckError = {
    code: string;
    detail?: string;
};

export type CheckResult = {
    error?: CheckError;
    latency_ms?: number;
    observed_ip?: string;
    reachable: boolean;
    unavailable?: boolean;
    verified: boolean;
};

export type ClearedBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    cleared: number;
};

export type Collection = {
    backdrop_path: string;
    external_ids: string;
    id: number;
    name: string;
    overview: string;
    poster_path: string;
    search_vector: unknown;
};

export type CollectionListResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListAllCollectionsRow> | null;
    total: number;
};

export type CollectionPartView = {
    local_media_item_id?: number;
    local_public_id?: string;
    local_slug?: string;
    poster_path?: string;
    title: string;
    tmdb_id?: number;
    vote_average?: number;
    year?: number;
};

export type CollectionResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    collection: Collection;
    genres: Array<string> | null;
    keywords: Array<string> | null;
    movies: Array<MediaItemCard> | null;
    owned_count: number;
    parts: Array<CollectionPartView> | null;
};

export type ComputeDevice = {
    description: string;
    name: string;
};

export type ContinueWatchingEnrichedRow = {
    entity_id: number;
    entity_type: string;
    episode_number: Int4;
    episode_title: Text;
    file_id: number;
    file_public_id?: string;
    id: number;
    library_id: number;
    media_item_id: number;
    media_item_public_id: string;
    media_type: string;
    poster_path: string;
    progress_seconds: number;
    season_number: Int4;
    slug: string;
    title: string;
    total_seconds: number;
    updated_at: Timestamptz;
};

export type CountLibraryFilesByStatusRow = {
    count: number;
    status: string;
};

export type Country = {
    iso_3166_1: string;
    name: string;
    stationcount: number;
};

export type CreateApiTokenRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * 0 means never expires
     */
    expires_in_days: number;
    /**
     * Human label so you can recognise the token
     */
    name: string;
};

export type CreateNativePlaybackGrantRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    audio_track?: number;
    file_id: string;
    /**
     * direct or hls; defaults to direct
     */
    mode?: string;
    quality?: string;
};

export type CreateUserListRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    description: string;
    /**
     * Smart-list filter spec, ignored for manual
     */
    filter_json: unknown;
    /**
     * manual (user-curated) or smart (filter-backed)
     */
    list_type: 'manual' | 'smart';
    media_type: 'movie' | 'tv' | 'music' | 'book' | 'comic' | 'podcast' | 'radio';
    name: string;
};

export type CreateApiTokenResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    created_at: string;
    expires_at?: string;
    id: number;
    last_seen_at: string;
    name: string;
    token: string;
};

export type CreateLibraryInputBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    media_type: 'movie' | 'tv' | 'anime' | 'music' | 'book' | 'comic' | 'podcast' | 'radio';
    name: string;
    /**
     * Absolute filesystem directory paths visible to the Heya host or container; mount network shares before configuring them
     */
    paths: Array<string> | null;
    settings?: LibrarySettings;
};

export type DnsStatus = {
    configured: boolean;
    error?: string;
    lan_host?: string;
    last_sync_at?: string;
    provider?: string;
    wan_host?: string;
    zone?: string;
};

export type DashboardStats = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    libraries: number;
    media_counts: {
        [key: string]: number;
    };
    missing_count: number;
    queue_pending: number;
    queue_running: number;
    total_files: number;
    total_media: number;
    total_people: number;
};

export type Date = {
    InfinityModifier: number;
    Time: string;
    Valid: boolean;
};

export type DeletedCountBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    deleted: number;
};

export type Device = {
    addr: string;
    capabilities?: Array<string> | null;
    host: string;
    id: string;
    last_seen: string;
    manufacturer?: string;
    media_origin?: string;
    model?: string;
    name: string;
    port: number;
    provider: string;
};

export type DevicesBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<Device> | null;
};

export type DoctorAppSection = {
    error?: string;
    go_version: string;
    goarch: string;
    goos: string;
    hostname: string;
    /**
     * Built from a working tree with uncommitted changes
     */
    modified?: boolean;
    num_cpu: number;
    pid: number;
    revision?: string;
    revision_time?: string;
    started_at?: string;
    uptime_seconds: number;
    version?: string;
};

export type DoctorConfigField = {
    env_var?: string;
    source: string;
    value: string;
};

export type DoctorConfigSection = {
    error?: string;
    fields: {
        [key: string]: DoctorConfigField;
    };
};

export type DoctorDatabaseSection = {
    database_name?: string;
    error?: string;
    /**
     * Latest applied goose migration id
     */
    migration_version?: number;
    pool: DoctorPoolStats;
    reachable: boolean;
    /**
     * Exact for libraries; estimated from pg_class.reltuples for the rest
     */
    row_counts?: {
        [key: string]: number;
    };
    version?: string;
};

export type DoctorLibrariesSection = {
    error?: string;
    libraries: Array<DoctorLibrary> | null;
    /**
     * Global count of media with no live file left (dashboard's cached missing_count) — not broken out per library because that anti-join is only cheap in aggregate
     */
    missing_media_total: number;
};

export type DoctorLibrary = {
    error?: string;
    file_count: number;
    /**
     * library_files grouped by status: pending/matched/unmatched/ignored/error
     */
    file_status_counts?: {
        [key: string]: number;
    };
    id: number;
    media_type: string;
    name: string;
    paths: Array<DoctorLibraryPath> | null;
};

export type DoctorLibraryPath = {
    error?: string;
    exists: boolean;
    /**
     * Configured filesystem path; URL credentials are redacted in legacy invalid values
     */
    path: string;
    readable: boolean;
};

export type DoctorLogsSection = {
    available: boolean;
    entries?: Array<Entry> | null;
    error?: string;
    note?: string;
};

export type DoctorPathUsage = {
    error?: string;
    exists: boolean;
    free_bytes?: number;
    path: string;
    total_bytes?: number;
    used_bytes?: number;
    used_pct?: number;
};

export type DoctorPoolStats = {
    acquired_connections: number;
    idle_connections: number;
    max_connections: number;
    total_connections: number;
};

export type DoctorQueueCount = {
    count: number;
    kind: string;
    state: string;
};

export type DoctorQueueSection = {
    counts?: Array<DoctorQueueCount> | null;
    error?: string;
};

export type DoctorReport = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    app: DoctorAppSection;
    config: DoctorConfigSection;
    database: DoctorDatabaseSection;
    generated_at: string;
    libraries: DoctorLibrariesSection;
    logs: DoctorLogsSection;
    queue: DoctorQueueSection;
    storage: DoctorStorageSection;
    tools: DoctorToolsSection;
};

export type DoctorStorageSection = {
    data_dir: DoctorPathUsage;
    error?: string;
    /**
     * Cached results from the last scan_library_disk run; empty until a scan completes. Never triggers a fresh walk.
     */
    library_disk_usage?: Array<LibraryDiskUsage> | null;
    transcode_dir: DoctorPathUsage;
    transcode_items?: number;
    transcode_used_mb?: number;
};

export type DoctorTool = {
    error?: string;
    found: boolean;
    path?: string;
    version?: string;
};

export type DoctorToolsSection = {
    error?: string;
    ffmpeg: DoctorTool;
    ffprobe: DoctorTool;
};

export type DownloadAssetRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    asset_type: 'poster' | 'backdrop' | 'logo' | 'art' | 'clearart' | 'banner' | 'thumb' | 'disc' | 'still';
    label?: string;
    url: string;
};

export type DownloadProgress = {
    bytes_done: number;
    bytes_total: number;
    current_file?: string;
    started_at: string;
};

export type EnrichedMediaBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    movies?: Array<EnrichedMovieView> | null;
    tv?: Array<EnrichedTvView> | null;
    /**
     * Echoes the requested ?type=
     */
    type: 'movie' | 'tv';
};

export type EnrichedMovieView = {
    audio_formats: Array<string> | null;
    available: boolean;
    backdrop_path: string;
    collection_id?: number;
    created_at: string;
    description: string;
    genres: Array<string> | null;
    id: number;
    library_id: number;
    media_type: string;
    original_language: string;
    poster_path: string;
    rating: number;
    release_date?: string;
    resolution?: string;
    runtime_minutes: number;
    slug: string;
    sort_title: string;
    title: string;
    updated_at: string;
    video_formats: Array<string> | null;
    year: string;
};

export type EnrichedTvView = {
    audio_formats: Array<string> | null;
    available: boolean;
    backdrop_path: string;
    created_at: string;
    description: string;
    first_air_date?: string;
    genres: Array<string> | null;
    id: number;
    last_air_date?: string;
    library_id: number;
    media_type: string;
    number_of_episodes: number;
    number_of_seasons: number;
    original_language: string;
    poster_path: string;
    rating: number;
    resolution?: string;
    slug: string;
    sort_title: string;
    status: string;
    title: string;
    updated_at: string;
    video_formats: Array<string> | null;
    year: string;
};

export type Entry = {
    fields?: {
        [key: string]: unknown;
    };
    level: string;
    message: string;
    source?: string;
    time: string;
};

export type ErrorDetail = {
    /**
     * Where the error occurred, e.g. 'body.items[3].tags' or 'path.thing-id'
     */
    location?: string;
    /**
     * Error message text
     */
    message?: string;
    /**
     * The value at the given location
     */
    value?: unknown;
};

export type ErrorModel = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * A human-readable explanation specific to this occurrence of the problem.
     */
    detail?: string;
    /**
     * Optional list of individual error details
     */
    errors?: Array<ErrorDetail> | null;
    /**
     * A URI reference that identifies the specific occurrence of the problem.
     */
    instance?: string;
    /**
     * HTTP status code
     */
    status?: number;
    /**
     * A short, human-readable summary of the problem type. This value should not change between occurrences of the error.
     */
    title?: string;
    /**
     * A URI reference to human-readable documentation for the error.
     */
    type?: string;
};

export type Event = {
    at: string;
    kind: string;
    level: string;
    message: string;
};

export type ExternalPlaylistSyncBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    enabled: boolean;
    playlist_id?: number;
};

export type ExternalPlaylistView = {
    description?: string;
    external_id: string;
    local_playlist_id?: number;
    name: string;
    sync_mode?: string;
    track_count: number;
    updated_at?: string;
    url?: string;
};

export type FacetsView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    analyzed_at?: string;
    analyzer_version: number;
    bpm?: number;
    bpm_confidence?: number;
    key?: KeyView;
    mood_tags?: {
        [key: string]: number;
    };
    top_genres?: Array<GenreScore> | null;
    track_id: number;
};

export type FavoritedBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    favorited: boolean;
};

export type FeatureDetails = {
    feature_id: number;
    feature_type: string;
    imdb_id: number;
    movie_name: string;
    title: string;
    tmdb_id: number;
    year: number;
};

export type FieldSource = {
    env_var?: string;
    source: string;
};

export type FileSegment = {
    end_ms: number;
    id: number;
    source: string;
    start_ms: number;
    type: string;
};

export type FileSegmentsResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    segments: Array<FileSegment> | null;
};

export type Float4 = {
    Float32: number;
    Valid: boolean;
};

export type ForYouItem = {
    available: boolean;
    id: number;
    media_type: string;
    public_id?: string;
    rating?: number;
    reason?: string;
    score: number;
    slug: string;
    title: string;
    year?: string;
};

export type ForYouResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    acquire?: Array<AcquireItem> | null;
    has_signal: boolean;
    items: Array<ForYouItem> | null;
};

export type FormFile = {
    ContentType: string;
    Filename: string;
    IsSet: boolean;
    Size: number;
};

export type FsBrowseBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    entries: Array<FsEntry> | null;
    parent?: string;
    path: string;
};

export type FsEntry = {
    is_dir: boolean;
    name: string;
    path: string;
};

export type FunnelBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    funnel: boolean;
};

export type GenreBucket = {
    artists: Array<BrowseBucketArtist> | null;
    label: string;
    name: string;
    parent: string;
    track_count: number;
};

export type GenreBucketsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<GenreBucket> | null;
};

export type GenreResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    genre: string;
    items: Array<MediaItemCard> | null;
    total: number;
    type_counts?: {
        [key: string]: number;
    };
};

export type GenreScore = {
    name: string;
    score: number;
};

export type GetUserStateRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    scope: 'movies' | 'series' | 'seasons' | 'episodes';
    series_id?: number;
};

export type GetMusicArtistBySlugRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    album_count: number;
    aliases: Array<string> | null;
    annotation: string;
    artist_type: string;
    available: boolean;
    begin_date: string;
    begin_year: number;
    biography: string;
    birthplace: string;
    cover_art_enriched_at: Timestamptz;
    deathday: string;
    disambiguation: string;
    discography_enriched_at: Timestamptz;
    end_date: string;
    ended: boolean;
    followers: number;
    genres: Array<string> | null;
    groups: string;
    id: number;
    listeners: number;
    media_item_id: number;
    media_item_public_id: string;
    members: string;
    metadata_sources: Array<string> | null;
    musicbrainz_id: string;
    name: string;
    playcount: number;
    popularity: number;
    poster_path: string;
    profiles: string;
    search_vector: unknown;
    slug: string;
    sort_name: string;
    tags: Array<string> | null;
    track_count: number;
    urls: string;
    wikipedia_links: string;
};

export type GetUserRatedTracksStatsRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artist_count: number;
    last_rated_at: Timestamptz;
    total_duration: number;
    track_count: number;
};

export type HttpMetrics = {
    bytes_received: number;
    bytes_sent: number;
    errors_per_second: number;
    errors_total: number;
    p50_latency_ms: number;
    p95_latency_ms: number;
    protocols: ProtocolStats;
    requests_in_flight: number;
    requests_per_second: number;
    requests_total: number;
};

export type HealthBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Database connection status
     */
    database: string;
    /**
     * Server status
     */
    status: string;
    /**
     * Build version (overridden at link time)
     */
    version: string;
};

export type HealthComponent = {
    /**
     * Populated only when OK is false (or when reporting an optional-but-disabled component)
     */
    message?: string;
    name: string;
    ok: boolean;
};

export type HomeSectionPref = {
    hidden?: boolean;
    id: string;
};

export type HomeSettings = {
    sections?: Array<HomeSectionPref> | null;
};

export type IdentifyBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    results: unknown;
};

export type IdentifySearchResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    results: Array<SearchResult> | null;
};

export type IdsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    ids: Array<number> | null;
};

export type ImageCatalogBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    models: Array<Model> | null;
};

export type ImageDownloadProgress = {
    bytes_done: number;
    bytes_total: number;
    current_file?: string;
    started_at: string;
};

export type ImageFetchBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    backend?: string;
    model?: string;
};

export type ImageGenerateBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    duration_ms: number;
    model: string;
    seed: number;
    url: string;
};

export type ImageStatus = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artifacts: Array<ArtifactStatus> | null;
    backend: string;
    build: string;
    device_error?: string;
    devices: Array<ComputeDevice> | null;
    download_bytes: number;
    download_error?: string;
    download_state: string;
    model: string;
    model_present: boolean;
    progress?: ImageDownloadProgress;
    runtime_present: boolean;
};

export type IngressMetrics = {
    bytes_sent: number;
    errors_total: number;
    name: string;
    p95_latency_ms: number;
    protocols: ProtocolStats;
    requests_in_flight: number;
    requests_per_second: number;
    requests_total: number;
};

export type IngressStatus = {
    by_ingress?: Array<IngressMetrics> | null;
    certificates?: Array<CertificateStatus> | null;
    generation: number;
    http: HttpMetrics;
    last_reload_at?: string;
    last_reload_error?: string;
    listeners: Array<ListenerStatus> | null;
    local_ca_root?: string;
    recent_events?: Array<Event> | null;
    running: boolean;
    started_at: string;
    updated_at: string;
    uptime_seconds: number;
    version: string;
};

export type Int2 = {
    Int16: number;
    Valid: boolean;
};

export type Int4 = {
    Int32: number;
    Valid: boolean;
};

export type Int8 = {
    Int64: number;
    Valid: boolean;
};

export type JellyfinConfigBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    enabled: boolean;
};

export type JellyfinCredentialBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    created_at: string;
    last_used_at?: string;
    pin: string;
    rotated_at: string;
};

export type JobKindSummaryRow = {
    count: number;
    kind: string;
};

export type JobListResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    has_more: boolean;
    jobs: Array<JobRow> | null;
    next_before_id?: number;
    total: number;
};

export type JobRow = {
    args: string;
    attempt: number;
    attempted_at?: string;
    created_at: string;
    errors?: string;
    finalized_at?: string;
    id: number;
    kind: string;
    max_attempts: number;
    queue: string;
    state: string;
};

export type JobSummaryRow = {
    count: number;
    state: string;
};

export type JobWorkerSetting = {
    default: number;
    env_var?: string;
    kind: string;
    label: string;
    locked: boolean;
    source: string;
    value: number;
};

export type JobWorkerSettings = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    restart_required: boolean;
    workers: Array<JobWorkerSetting> | null;
};

export type JobWorkerUpdate = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    workers: {
        [key: string]: number;
    };
};

export type KeyView = {
    camelot: string;
    clarity: number;
    display: string;
    mode: string;
    root: string;
};

export type KeywordResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<MediaItemCard> | null;
    keyword: string;
    total: number;
    type_counts?: {
        [key: string]: number;
    };
};

export type LanguageInfo = {
    code: string;
    count: number;
};

export type LapsedArtistEntry = {
    albums: Array<ListAlbumsByArtistIdForShelfRow> | null;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    last_played_at: string;
    media_item_id: number;
    months_lapsed: number;
    play_count: number;
};

export type LapsedShelfBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artists: Array<LapsedArtistEntry> | null;
    enabled: boolean;
    since_label: string;
};

export type LastfmAuthCompleteRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    token: string;
};

export type LastfmAuthStartBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    auth_url: string;
    token: string;
};

export type LibraryScannerApproveCandidateRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    candidate_id: number;
};

export type LibraryScannerAssignIdentityRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    confidence?: number;
    description?: string;
    external_ids?: {
        [key: string]: string;
    };
    heya_slug?: string;
    poster_url?: string;
    provider_id: string;
    provider_name?: string;
    title?: string;
    year?: string;
};

export type LibraryScannerBulkApproveSingleRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    min_confidence: number;
};

export type LibraryScannerIgnoreIdentityRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    reason?: string;
};

export type LibraryScannerRejectIdentityRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    reason?: string;
};

export type LibraryDiskUsage = {
    bytes: number;
    file_count: number;
    library_id: number;
    path: string;
    scanned_at: string;
};

export type LibraryFile = {
    audio_formats: Array<string> | null;
    content_hash: string;
    created_at: Timestamptz;
    deleted_at: Timestamptz;
    error_message: string;
    has_trickplay: boolean;
    id: number;
    keyframes: string;
    library_id: number;
    media_info: string;
    media_item_id: Int8;
    mtime: Timestamptz;
    parse_result: string;
    path: string;
    public_id: string;
    segments_analyzed_at: Timestamptz;
    segments_detected_at: Timestamptz;
    size: number;
    status: string;
    updated_at: Timestamptz;
    video_formats: Array<string> | null;
    video_height: number;
};

export type LibraryPlaybackOv = {
    default_audio_language?: string;
    default_subtitle_language?: string;
    subtitle_mode?: string;
    subtitle_priority?: Array<string> | null;
};

export type LibrarySettings = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    auto_collections: boolean;
    enable_trickplay: boolean;
    fetch_ratings: boolean;
    generate_thumbnails: boolean;
    match_threshold?: number;
    preferred_country: string;
    preferred_language: string;
    save_images: boolean;
    save_nfo: boolean;
    use_local_data: boolean;
    watch: boolean;
};

export type LibrarySettingsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    defaults: LibrarySettings;
    settings: LibrarySettings;
};

export type LibraryView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    created_by: number;
    id: number;
    media_type: string;
    name: string;
    paths: Array<string> | null;
    settings: LibrarySettings;
    sources: LibraryViewSources;
};

export type LibraryViewSources = {
    media_type?: FieldSource;
    name?: FieldSource;
    paths?: FieldSource;
};

export type ListAlbumsByArtistIdForShelfRow = {
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artwork: string;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    track_count: number;
    true_peak_db: Numeric;
    year: string;
};

export type ListAlbumsByArtistSlugRow = {
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artwork: string;
    available: boolean;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    track_count: number;
    true_peak_db: Numeric;
    year: string;
};

export type ListAlbumsByLabelRow = {
    album_cover_path: string;
    album_id: number;
    album_label: string;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
};

export type ListAllCollectionsRow = {
    backdrop_path: string;
    external_ids: string;
    id: number;
    movie_count: number;
    name: string;
    overview: string;
    poster_path: string;
    search_vector: unknown;
};

export type ListAllGenresRow = {
    count: number;
    genre: unknown;
};

export type ListArtistTopTracksForMixRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    play_count: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListArtistTracksTopPlayedFirstRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    track_id: number;
    track_number: number;
    track_title: string;
    user_play_count: number;
};

export type ListArtistsByGenreRow = {
    album_count: number;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    media_item_id: number;
    media_item_public_id: string;
    poster_path: string;
    track_count: number;
};

export type ListCollectionsWithLocalMediaRow = {
    id: number;
    movie_count: number;
    name: string;
    poster_path: string;
};

export type ListMusicAlbumsRow = {
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artwork: string;
    available: boolean;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    track_count: number;
    true_peak_db: Numeric;
    year: string;
};

export type ListMusicArtistsRow = {
    album_count: number;
    aliases: Array<string> | null;
    annotation: string;
    artist_type: string;
    available: boolean;
    begin_date: string;
    begin_year: number;
    biography: string;
    birthplace: string;
    cover_art_enriched_at: Timestamptz;
    deathday: string;
    disambiguation: string;
    discography_enriched_at: Timestamptz;
    end_date: string;
    ended: boolean;
    followers: number;
    genres: Array<string> | null;
    groups: string;
    id: number;
    listeners: number;
    media_item_id: number;
    media_item_public_id: string;
    members: string;
    metadata_sources: Array<string> | null;
    musicbrainz_id: string;
    name: string;
    playcount: number;
    popularity: number;
    poster_path: string;
    profiles: string;
    search_vector: unknown;
    slug: string;
    sort_name: string;
    tags: Array<string> | null;
    track_count: number;
    urls: string;
    wikipedia_links: string;
};

export type ListMusicTracksRow = {
    album_cover_path: string;
    album_genres: Array<string> | null;
    album_id: number;
    album_label: string;
    album_release_date: Date;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artists_display: string;
    available: boolean;
    bit_depth: Int4;
    bitrate_kbps: Int4;
    bpm: Float4;
    channels: Int4;
    composer: string;
    disc_number: number;
    duration: number;
    explicit: boolean;
    format: Text;
    integrated_lufs: Numeric;
    key_mode: Int2;
    key_root: Int2;
    last_played_at: Timestamptz;
    library_added_at: Timestamptz;
    play_count: number;
    rating: Int2;
    sample_rate_hz: Int4;
    size_bytes: Int8;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListOnThisDayAlbumsRow = {
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artwork: string;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    release_year: number;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    track_count: number;
    true_peak_db: Numeric;
    year: string;
};

export type ListPlaylistTracksRow = {
    added_at: Timestamptz;
    album_cover_path: string;
    album_genres: Array<string> | null;
    album_id: number;
    album_label: string;
    album_release_date: Date;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artists_display: string;
    available: boolean;
    bit_depth: Int4;
    bitrate_kbps: Int4;
    bpm: Float4;
    channels: Int4;
    composer: string;
    disc_number: number;
    duration: number;
    explicit: boolean;
    format: Text;
    integrated_lufs: Numeric;
    key_mode: Int2;
    key_root: Int2;
    last_played_at: Timestamptz;
    library_added_at: Timestamptz;
    play_count: number;
    position: number;
    rating: Int2;
    sample_rate_hz: Int4;
    size_bytes: Int8;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListRadioRecentsRow = {
    bitrate: number;
    codec: string;
    country: string;
    favicon: string;
    id: number;
    name: string;
    played_at: Timestamptz;
    stationuuid: string;
    tags: string;
    url: string;
    user_id: number;
};

export type ListRecentUserPlaylistsRow = {
    auto_album_slug: unknown;
    auto_artist_slug: unknown;
    cover_path: string;
    created_at: Timestamptz;
    description: string;
    has_cover: boolean;
    id: number;
    last_activity_at: unknown;
    last_played_at: unknown;
    name: string;
    slug: string;
    track_count: number;
    updated_at: Timestamptz;
};

export type ListRecentlyAddedAlbumsRow = {
    added_at: Timestamptz;
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artwork: string;
    available: boolean;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    track_count: number;
    true_peak_db: Numeric;
    year: string;
};

export type ListRecentlyPlayedArtistsRow = {
    album_count: number;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    available: boolean;
    last_played_at: Timestamptz;
    media_item_id: number;
    media_item_public_id: string;
    poster_path: string;
    track_count: number;
};

export type ListRecentlyPlayedTracksRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    played_at: Timestamptz;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListRecentlyWatchedEpisodesRow = {
    episode_id: number;
    episode_number: number;
    episode_title: string;
    library_id: number;
    media_item_id: number;
    media_item_public_id: string;
    season_number: number;
    series_slug: string;
    series_title: string;
    updated_at: Timestamptz;
};

export type ListRecentlyWatchedRow = {
    entity_id: number;
    entity_type: string;
    id: number;
    library_id: number;
    media_item_id: number;
    media_item_public_id: string;
    media_type: string;
    poster_path: string;
    slug: string;
    title: string;
    updated_at: Timestamptz;
};

export type ListTracksByArtistSlugRow = {
    album_cover_path: string;
    album_id: number;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    available: boolean;
    disc_number: number;
    duration: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListTracksByGenreRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    score: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListTracksByMoodRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    score: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListTracksByTempoBandRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    bpm: Float4;
    disc_number: number;
    duration: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListUserLovedAlbumsRow = {
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artwork: string;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    loved_at: Timestamptz;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    track_count: number;
    true_peak_db: Numeric;
    year: string;
};

export type ListUserLovedArtistsRow = {
    album_count: number;
    aliases: Array<string> | null;
    annotation: string;
    artist_type: string;
    begin_date: string;
    begin_year: number;
    biography: string;
    birthplace: string;
    cover_art_enriched_at: Timestamptz;
    deathday: string;
    disambiguation: string;
    discography_enriched_at: Timestamptz;
    end_date: string;
    ended: boolean;
    followers: number;
    genres: Array<string> | null;
    groups: string;
    id: number;
    listeners: number;
    loved_at: Timestamptz;
    media_item_id: number;
    members: string;
    metadata_sources: Array<string> | null;
    musicbrainz_id: string;
    name: string;
    playcount: number;
    popularity: number;
    poster_path: string;
    profiles: string;
    search_vector: unknown;
    slug: string;
    sort_name: string;
    tags: Array<string> | null;
    track_count: number;
    urls: string;
    wikipedia_links: string;
};

export type ListUserLovedTracksRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    available: boolean;
    disc_number: number;
    duration: number;
    loved_at: Timestamptz;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListUserPlaylistsRow = {
    auto_album_slug: unknown;
    auto_artist_slug: unknown;
    cover_path: string;
    created_at: Timestamptz;
    description: string;
    has_cover: boolean;
    id: number;
    name: string;
    pinned: boolean;
    sidebar_pinned: boolean;
    sidebar_position: number;
    slug: string;
    sync_services: unknown;
    tags: Array<string> | null;
    track_count: number;
    updated_at: Timestamptz;
    user_id: number;
};

export type ListUserRatedAlbumsRow = {
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artwork: string;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rated_at: Timestamptz;
    rating: Numeric;
    rating_2: number;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    true_peak_db: Numeric;
    year: string;
};

export type ListUserRatedArtistsRow = {
    album_count: number;
    aliases: Array<string> | null;
    annotation: string;
    artist_type: string;
    begin_date: string;
    begin_year: number;
    biography: string;
    birthplace: string;
    cover_art_enriched_at: Timestamptz;
    deathday: string;
    disambiguation: string;
    discography_enriched_at: Timestamptz;
    end_date: string;
    ended: boolean;
    followers: number;
    genres: Array<string> | null;
    groups: string;
    id: number;
    listeners: number;
    media_item_id: number;
    media_item_public_id: string;
    members: string;
    metadata_sources: Array<string> | null;
    musicbrainz_id: string;
    name: string;
    playcount: number;
    popularity: number;
    poster_path: string;
    profiles: string;
    rated_at: Timestamptz;
    rating: number;
    search_vector: unknown;
    slug: string;
    sort_name: string;
    tags: Array<string> | null;
    track_count: number;
    urls: string;
    wikipedia_links: string;
};

export type ListUserRatedTracksRow = {
    album_cover_path: string;
    album_genres: Array<string> | null;
    album_id: number;
    album_label: string;
    album_release_date: Date;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    artists_display: string;
    available: boolean;
    bit_depth: Int4;
    bitrate_kbps: Int4;
    bpm: Float4;
    channels: Int4;
    composer: string;
    disc_number: number;
    duration: number;
    explicit: boolean;
    format: Text;
    integrated_lufs: Numeric;
    key_mode: Int2;
    key_root: Int2;
    last_played_at: Timestamptz;
    library_added_at: Timestamptz;
    play_count: number;
    rated_at: Timestamptz;
    rating: number;
    sample_rate_hz: Int4;
    size_bytes: Int8;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type ListenerStatus = {
    active: boolean;
    address: string;
    description?: string;
    error?: string;
    kind: string;
    name: string;
    network: string;
    protocols: Array<string> | null;
    public: boolean;
    tls: boolean;
};

export type ListeningStats = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    mood_avg: Array<TopUserMoodsRow> | null;
    tempo_histogram: Array<UserTempoHistogramRow> | null;
    top_genres: Array<TopUserGenresRow> | null;
    total_plays: number;
};

export type LiveBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Always 'ok' when the process is alive
     */
    status: string;
};

export type LocalModel = {
    file: string;
    id: string;
    label: string;
    notes?: string;
    /**
     * rough resident footprint at the default context size
     */
    ram_hint: string;
    sha256: string;
    size: number;
    url: string;
};

export type LoginInputBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Password
     */
    password: string;
    /**
     * Username
     */
    username: string;
};

export type LovedBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    loved: boolean;
};

export type LyricsLine = {
    text: string;
    time_ms: number;
};

export type LyricsResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    lines: Array<LyricsLine> | null;
    synced: boolean;
};

export type MarkMediaWatchedRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    watched: boolean;
};

export type MarkSeasonWatchedRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    watched: boolean;
};

export type MatchCandidate = {
    chosen: boolean;
    confidence: Numeric;
    created_at: Timestamptz;
    description: string;
    id: number;
    library_file_id: number;
    poster_url: string;
    provider_id: string;
    provider_name: string;
    raw_data: string;
    title: string;
    year: string;
};

export type MediaAsset = {
    aspect: string;
    asset_type: string;
    content_hash: string;
    created_at: Timestamptz;
    file_size: number;
    height: number;
    id: number;
    label: string;
    language: string;
    likes: number;
    local_path: string;
    media_item_id: number;
    remote_url: string;
    score: Numeric;
    sort_order: number;
    source: string;
    visual_hash: string;
    width: number;
};

export type MediaFileInfo = {
    bit_rate?: number;
    container?: string;
    duration?: number;
    filename: string;
    id: number;
    path: string;
    size: number;
    streams?: Array<MediaFileStream> | null;
};

export type MediaFileStream = {
    bit_rate?: string;
    channel_layout?: string;
    channels?: number;
    codec_long_name?: string;
    codec_name: string;
    codec_type: string;
    color_space?: string;
    default: boolean;
    forced: boolean;
    height?: number;
    index: number;
    language?: string;
    pix_fmt?: string;
    profile?: string;
    sample_rate?: string;
    title?: string;
    width?: number;
};

export type MediaItemCard = {
    backdrop_path: string;
    base_enriched_at: Timestamptz;
    created_at: Timestamptz;
    description: string;
    enrichment_status: string;
    external_ids: string;
    extras_enriched_at: Timestamptz;
    field_provenance: string;
    heya_enriched_at: Timestamptz;
    heya_slug: string;
    homepage: string;
    id: number;
    images_enriched_at: Timestamptz;
    last_enrich_attempt_at: Timestamptz;
    last_enrich_error: string;
    library_id: number;
    match_confidence: number;
    matched_at: Timestamptz;
    media_type: string;
    metadata_refreshed_at: Timestamptz;
    original_language: string;
    original_title: string;
    people_enriched_at: Timestamptz;
    poster_path: string;
    provider_kind: string;
    public_id: string;
    search_vector: unknown;
    slug: string;
    slug_locked: boolean;
    sort_title: string;
    status: string;
    structure_enriched_at: Timestamptz;
    tagline: string;
    title: string;
    updated_at: Timestamptz;
    year: string;
};

export type MediaItemView = {
    available: boolean;
    backdrop_path: string;
    base_enriched_at: Timestamptz;
    book_author?: string;
    book_format?: string;
    created_at: Timestamptz;
    description: string;
    enrichment_status: string;
    external_ids: string;
    extras_enriched_at: Timestamptz;
    field_provenance: string;
    heya_enriched_at: Timestamptz;
    heya_slug: string;
    homepage: string;
    id: number;
    images_enriched_at: Timestamptz;
    last_enrich_attempt_at: Timestamptz;
    last_enrich_error: string;
    library_id: number;
    match_confidence: number;
    matched_at: Timestamptz;
    media_type: string;
    metadata_refreshed_at: Timestamptz;
    original_language: string;
    original_title: string;
    people_enriched_at: Timestamptz;
    poster_path: string;
    provider_kind: string;
    public_id: string;
    search_vector: unknown;
    slug: string;
    slug_locked: boolean;
    sort_title: string;
    status: string;
    structure_enriched_at: Timestamptz;
    tagline: string;
    title: string;
    updated_at: Timestamptz;
    year: string;
};

export type MediaLanguages = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    audio_languages: Array<LanguageInfo> | null;
    subtitle_languages: Array<LanguageInfo> | null;
};

export type MediaStateBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    favorited: Array<number> | null;
    watched: Array<number> | null;
};

export type Message = {
    content: string;
    /**
     * system | user | assistant
     */
    role: string;
};

export type MetadataQueueRecent = {
    avg_duration_sec: number;
    completed_5min: number;
};

export type MetadataQueueRunning = {
    item_id?: number;
    item_title?: string;
    job_id: number;
    kind: string;
    media_type?: string;
    priority: number;
    source?: string;
    started_at: string;
};

export type MetadataQueueStatus = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    pending: number;
    pending_by_priority: {
        [key: string]: number;
    };
    recent: MetadataQueueRecent;
    running?: MetadataQueueRunning;
};

export type MissingMediaItem = {
    id: number;
    media_type: string;
    poster_path: string;
    slug: string;
    title: string;
    year: string;
};

export type MixToBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<MixToTracksRow> | null;
};

export type MixToTracksRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    bpm: Float4;
    disc_number: number;
    distance: number;
    duration: number;
    key_mode: Int2;
    key_root: Int2;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type MixesBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<MusicMix> | null;
};

export type Model = {
    artifacts: Array<ModelArtifact> | null;
    default_cfg: number;
    default_height: number;
    default_memory_mode: string;
    default_steps: number;
    default_width: number;
    flow_shift: number;
    id: string;
    label: string;
    license: string;
    ram_hint: string;
    sampling_method: string;
    scheduler: string;
};

export type ModelArtifact = {
    name: string;
    role: string;
    sha256: string;
    shared_llm_file?: string;
    size: number;
    url: string;
};

export type MoodBucket = {
    artists: Array<BrowseBucketArtist> | null;
    key: string;
    label: string;
    threshold: number;
    track_count: number;
};

export type MoodBucketsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<MoodBucket> | null;
};

export type MoreByArtist = {
    albums: Array<ListAlbumsByArtistIdForShelfRow> | null;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    media_item_id: number;
};

export type MoreByArtistsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<MoreByArtist> | null;
};

export type MoreFromLabelBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    albums: Array<ListAlbumsByLabelRow> | null;
    enabled: boolean;
    label: string;
};

export type MoreInGenreBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artists: Array<ListArtistsByGenreRow> | null;
    enabled: boolean;
    genre: string;
};

export type MostPlayedAlbumsInRangeRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    play_count: number;
};

export type MostPlayedBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    albums: Array<MostPlayedAlbumsInRangeRow> | null;
    enabled: boolean;
    window_label: string;
};

export type MusicAlbumDetail = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    album: Album;
    artist: ArtistView;
    artist_slug: string;
    artwork?: Array<AlbumArtworkRef> | null;
    editions?: Array<AlbumEdition> | null;
    media_item_id: number;
    media_item_public_id?: string;
    ratings?: Array<AlbumRating> | null;
    release_events?: Array<AlbumReleaseEvent> | null;
    tracks: Array<TrackView> | null;
};

export type MusicCatalogSuggestion = {
    artist_name: string;
    provider_url?: string;
    reason: string;
    recording_entity_id: string;
    score: number;
    title: string;
};

export type MusicCounts = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    albums: number;
    artists: number;
    tracks: number;
};

export type MusicHomeData = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    recent_albums: Array<ListRecentlyAddedAlbumsRow> | null;
    recent_artists: Array<RecentArtistEntry> | null;
};

export type MusicListPageListAlbumsByArtistSlugRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListAlbumsByArtistSlugRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListMusicAlbumsRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListMusicAlbumsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListMusicArtistsRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListMusicArtistsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListMusicTracksRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListMusicTracksRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByArtistSlugRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListTracksByArtistSlugRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByGenreRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListTracksByGenreRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByMoodRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListTracksByMoodRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByTempoBandRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListTracksByTempoBandRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserLovedAlbumsRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListUserLovedAlbumsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserLovedArtistsRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListUserLovedArtistsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserLovedTracksRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListUserLovedTracksRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserRatedAlbumsRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListUserRatedAlbumsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserRatedArtistsRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListUserRatedArtistsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserRatedTracksRow = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListUserRatedTracksRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicMix = {
    description: string;
    kind: string;
    name: string;
    seed_artist_id: number;
    seed_artist_media_item_id: number;
    seed_artist_media_item_public_id?: string;
    seed_artist_name: string;
    seed_artist_slug: string;
    seed_genre?: string;
    slug: string;
    tracks: Array<ListArtistTopTracksForMixRow> | null;
};

export type MusicServiceUpdate = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    scrobble_enabled?: boolean;
    /**
     * ListenBrainz user token; empty keeps the stored one
     */
    token?: string;
    username?: string;
};

export type MusicServiceView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    import_state: unknown;
    scrobble_enabled: boolean;
    service: 'listenbrainz' | 'lastfm';
    token_set: boolean;
    username: string;
};

export type MusicServicesBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    services: Array<MusicServiceView> | null;
};

export type MusicTrackDetail = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    album_cover_path: string;
    album_id: number;
    album_integrated_lufs: Numeric;
    album_slug: string;
    album_title: string;
    album_true_peak_db: Numeric;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    explicit: boolean;
    file_path: string;
    files: Array<TrackFile> | null;
    id: number;
    isrc: string;
    lyrics_available: boolean;
    lyrics_path: string;
    recording_mbid: string;
    title: string;
    track_number: number;
};

export type NativePlaybackGrantBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    expires_at_unix_millis: number;
    header_name: string;
    media_path: string;
    playback_grant: string;
};

export type Numeric = {
    Exp: number;
    InfinityModifier: number;
    Int: string | null;
    NaN: boolean;
    Valid: boolean;
};

export type OkBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    ok: boolean;
};

export type OnThisDayBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListOnThisDayAlbumsRow> | null;
};

export type OpensubtitlesDownloadRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    file_id: number;
    file_name: string;
    language: string;
    media_item_id: number;
};

export type OsCredentials = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * OpenSubtitles API key
     */
    api_key: string;
    password: string;
    username: string;
};

export type OsDownloadBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    asset: MediaAsset;
    remaining: number;
    status: string;
};

export type OsTestBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    error?: string;
    ok: boolean;
    user?: unknown;
};

export type PeopleMediaIdsRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    person_ids: Array<number> | null;
};

export type PlaybackDecision = {
    action: string;
    copy_audio: boolean;
    copy_video: boolean;
    deinterlace?: boolean;
    downmix_stereo?: boolean;
    fix_anamorphic?: boolean;
    needs_fmp4: boolean;
    needs_tonemap: boolean;
    profile: string;
    reason: string;
    reason_bits: number;
    reasons: Array<string> | null;
    retag_dovi?: string;
    retag_hevc?: boolean;
    rotate?: number;
    strip_dovi_el?: boolean;
};

export type PlaybackEvent = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Whether playback reached its natural end
     */
    completed: boolean;
    /**
     * Movie media_item id, episode id, or track id
     */
    entity_id: number;
    /**
     * What's being played
     */
    entity_type: 'movie' | 'episode' | 'track';
    /**
     * How far into the item the player is
     */
    position_seconds: number;
    /**
     * Origin label: queue | radio | album | playlist | search | browse | similar
     */
    source?: string;
    /**
     * UTC Unix time when this playback began (track completion only)
     */
    started_at_unix?: number;
    /**
     * Total length (0 if unknown)
     */
    total_seconds: number;
};

export type PlaybackPrefBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    audio_language: string;
    media_item_id: number;
    subtitle_language: string;
    subtitle_mode: string;
};

export type PlaybackSettings = {
    default_audio_language: string;
    default_quality: string;
    default_subtitle_language: string;
    library_overrides: {
        [key: string]: LibraryPlaybackOv;
    };
    subtitle_mode: string;
    subtitle_priority: Array<string> | null;
};

export type PlaylistCollectionView = {
    auto_sync: boolean;
    description?: string;
    key: string;
    name: string;
    playlists: Array<ExternalPlaylistView> | null;
};

export type PlaylistDetail = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    has_cover: boolean;
    playlist: UserPlaylist;
    syncs: Array<PlaylistSyncView> | null;
    tracks: Array<ListPlaylistTracksRow> | null;
};

export type PlaylistMutation = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    description: string;
    name: string;
    /**
     * Free-form organization tags; omit to keep existing
     */
    tags?: Array<string> | null;
};

export type PlaylistServiceCatalog = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    capabilities: Capabilities;
    collections: Array<PlaylistCollectionView> | null;
    playlists: Array<ExternalPlaylistView> | null;
    service: string;
};

export type PlaylistSyncToggle = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    enabled: boolean;
    mode?: 'two_way' | 'pull_only';
};

export type PlaylistSyncView = {
    external_id: string;
    external_url?: string;
    last_error?: string;
    last_synced_at?: string;
    service: string;
    sync_mode: string;
};

export type PlaylistsListBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListUserPlaylistsRow> | null;
};

export type Podcast = {
    artwork_url: string;
    author: string;
    categories: {
        [key: string]: string;
    };
    description: string;
    episode_count: number;
    feed_url: string;
    id: number;
    language: string;
    title: string;
};

export type PodcastCategoriesBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<Category> | null;
};

export type PodcastContinueBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<UserPodcastProgress> | null;
};

export type PodcastDetail = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artwork_url: string;
    author: string;
    categories: Array<string> | null;
    description: string;
    episodes: Array<PodcastEpisode> | null;
    feed_url: string;
    language: string;
    link: string;
    title: string;
};

export type PodcastEpisode = {
    artwork_url?: string;
    audio_size: number;
    audio_type: string;
    audio_url: string;
    description: string;
    duration_secs: number;
    episode_number?: number;
    guid: string;
    pub_date: string;
    season_number?: number;
    title: string;
};

export type PodcastProgressInput = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artwork_url?: string;
    audio_url: string;
    completed: boolean;
    episode_guid: string;
    feed_url: string;
    progress_seconds: number;
    title: string;
    total_seconds: number;
};

export type PodcastSubsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<UserPodcastSubscription> | null;
};

export type PodcastsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<Podcast> | null;
};

export type PortMappingStatus = {
    active: boolean;
    error?: string;
    external_port: number;
    internal_ip?: string;
    internal_port: number;
    lease_seconds: number;
    mapped_at?: string;
    protocol: string;
};

export type ProbeBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    challenge: string;
};

export type ProtocolStats = {
    http1: number;
    http2: number;
    http3: number;
};

export type Provider = {
    base_url: string;
    id: string;
    label: string;
    needs_key: boolean;
};

export type QualityOption = {
    height: number;
    label: string;
};

export type QuerySnapshot = {
    average_ms: number;
    in_flight: number;
    max_ms: number;
    p50_ms: number;
    p95_ms: number;
    queries_per_second: number;
    recent_errors: number;
    started_at: string;
    top_statements: Array<QueryStatement> | null;
    total_errors: number;
    total_queries: number;
    tracked_statements: number;
    window_seconds: number;
};

export type QueryStatement = {
    average_ms: number;
    calls: number;
    errors: number;
    last_error_at?: string;
    last_error_code?: string;
    last_seen_at: string;
    max_ms: number;
    recent_average_ms: number;
    recent_calls: number;
    recent_errors: number;
    recent_p95_ms: number;
    rows: number;
    statement: string;
    total_duration_ms: number;
};

export type QueueAdvanceRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * The item this renderer just finished/skipped — makes double-fires no-ops
     */
    from_item_id: number;
    reason: 'ended' | 'skip' | 'prev';
};

export type QueueClaimRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * local:<client_id> or cast:<device_id>
     */
    output: string;
};

export type QueueEnqueueRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    at?: 'end' | 'next';
    track_ids: Array<number> | null;
};

export type QueueHeartbeatRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * This renderer's output id
     */
    output: string;
    playing: boolean;
    position_seconds: number;
};

export type QueueJumpRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    item_id: number;
};

export type QueueMoveItemRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Place after this item (0 = right after the current track)
     */
    after_item_id?: number;
};

export type QueueRepeatRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    mode: 'off' | 'all' | 'one';
};

export type QueueReplaceRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Claiming output id, e.g. local:<client_id>
     */
    output?: string;
    shuffle?: boolean;
    source: QueueSource;
    /**
     * Track to point at first (0 = head)
     */
    start_track_id?: number;
};

export type QueueShuffleRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    on: boolean;
};

export type QueueItemView = {
    album_id: number;
    album_slug: string;
    album_title: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    item_id: number;
    ord: number;
    title: string;
    track_id: number;
    track_number: number;
};

export type QueueSource = {
    /**
     * Genre name for kind=genre
     */
    genre?: string;
    /**
     * album/artist/playlist id (kind-dependent)
     */
    id?: number;
    /**
     * What to materialize from
     */
    kind: 'album' | 'artist' | 'playlist' | 'genre' | 'library' | 'tracks';
    /**
     * Explicit tracks for kind=tracks (mixes, selections)
     */
    track_ids?: Array<number> | null;
};

export type QueueView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    active_output?: string;
    current_index: number;
    current_item_id?: number;
    items: Array<QueueItemView> | null;
    playing: boolean;
    position_seconds: number;
    repeat_mode: string;
    shuffled: boolean;
    source?: QueueSource;
    total: number;
    version: number;
    window_start_index: number;
};

export type QuickSearchResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    buckets: {
        [key: string]: SearchBucket;
    };
    query: string;
};

export type RadioCountriesBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<Country> | null;
};

export type RadioFavoritesBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<UserRadioFavorite> | null;
};

export type RadioRecentsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListRadioRecentsRow> | null;
};

export type RadioRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Tracks to skip (typically the current queue)
     */
    exclude_track_ids?: Array<number> | null;
    /**
     * 0..1 knob for how strongly candidates must share the seed's genre(s) to rank well. 0 (default) is a no-op; near 1 pushes zero-genre-overlap candidates to the bottom and, at >=0.9, drops them once enough overlapping candidates remain to fill the limit.
     */
    genre_affinity?: number;
    /**
     * Number of tracks to return
     */
    limit: number;
    seed: RadioSeed;
    /**
     * Optional. When populated, every seed is resolved to a track and their sonic embeddings are averaged into a centroid for KNN. Use to mix multiple artists/albums/tracks/vibes into one cohesive queue.
     */
    seeds?: Array<RadioSeed> | null;
};

export type RadioResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    seed_track_id: number;
    /**
     * Similar canonical recordings that are not currently playable in this library
     */
    suggestions: Array<MusicCatalogSuggestion> | null;
    tracks: Array<SimilarTracksByTrackRichRow> | null;
};

export type RadioSeed = {
    /**
     * Required when kind=album
     */
    album_id?: number;
    /**
     * Required when kind=artist (or pass artist_slug)
     */
    artist_id?: number;
    /**
     * Alternative to artist_id for kind=artist
     */
    artist_slug?: string;
    /**
     * Seed type — picks how Heya resolves the starting track
     */
    kind: 'track' | 'artist' | 'album' | 'text';
    /**
     * Required when kind=text (CLAP audio-vibe prompt)
     */
    text?: string;
    /**
     * Required when kind=track
     */
    track_id?: number;
};

export type RadioTagsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<Tag> | null;
};

export type RailPageBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    has_more: boolean;
    items: Array<RecRailItem> | null;
};

export type RatingBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    rating: number;
};

export type ReadyBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    components: Array<HealthComponent> | null;
    /**
     * 'ok' when all components healthy, 'degraded' otherwise
     */
    status: string;
};

export type RecItem = {
    external_ids: {
        [key: string]: string;
    };
    local_media_item_id?: number;
    local_poster_path?: string;
    local_slug?: string;
    media_type: string;
    poster_path: string;
    provider_score?: number;
    release_date: string;
    source_count: number;
    title: string;
    vote_average: unknown;
};

export type RecRail = {
    baseline?: string;
    baseline_id?: number;
    items: Array<RecRailItem> | null;
    key: string;
    subtitle?: string;
    title: string;
};

export type RecRailItem = {
    available: boolean;
    id: number;
    media_type: string;
    rating?: number;
    slug: string;
    sub?: string;
    title: string;
    year?: string;
};

export type RecentAlbumsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListRecentlyAddedAlbumsRow> | null;
};

export type RecentArtistEntry = {
    added_at: string;
    album_count: number;
    id: number;
    /**
     * new = artist first appeared with this event, updated = new releases were added to an existing artist
     */
    kind: 'new' | 'updated';
    latest_album_slug?: string;
    latest_album_title?: string;
    media_item_id: number;
    media_item_public_id?: string;
    name: string;
    new_album_count: number;
    slug: string;
    track_count: number;
};

export type RecentArtistsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListRecentlyPlayedArtistsRow> | null;
};

export type RecentPlaylistsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListRecentUserPlaylistsRow> | null;
};

export type RecentlyAddedTvEntry = {
    added_at: string;
    description?: string;
    episode_count: number;
    episode_number: number;
    episode_title?: string;
    /**
     * series = brand-new show, season = brand-new season, episodes = several episodes added to an existing season, episode = a single new episode
     */
    kind: 'series' | 'season' | 'episodes' | 'episode';
    media_item_id: number;
    media_item_public_id?: string;
    season_count: number;
    season_number: number;
    slug: string;
    title: string;
};

export type RecentlyPlayedBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ListRecentlyPlayedTracksRow> | null;
};

export type RecommendationsMlSettings = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    accelerator: string;
    enabled: boolean;
};

export type RecommendedResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    rails: Array<RecRail> | null;
};

export type RecordingCredit = {
    artist_entity_id?: string;
    artist_mbid?: string;
    artist_name: string;
    attributes?: Array<string> | null;
    role: string;
};

export type RegisterInputBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Email address
     */
    email: string;
    /**
     * Password
     */
    password: string;
    /**
     * Username
     */
    username: string;
};

export type RemoteConfigPayload = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    acme_email?: string;
    /**
     * DNS provider for hostnames + certificates
     */
    dns_provider?: '' | 'desec' | 'duckdns' | 'cloudflare';
    /**
     * Provider API token (write-only; empty keeps existing)
     */
    dns_token?: string;
    /**
     * Zone managed at the provider (myname.dedyn.io, example.com)
     */
    domain?: string;
    enabled: boolean;
    /**
     * External+listener port; 0 = keep current / auto-generate
     */
    port?: number;
    /**
     * Optional label under the domain (heya → wan.heya.example.com)
     */
    subdomain?: string;
};

export type RemoteConfigView = {
    acme_email?: string;
    dns_provider?: string;
    domain?: string;
    enabled: boolean;
    port: number;
    subdomain?: string;
    token_set: boolean;
};

export type RemoteStatus = {
    cert: CertStatus;
    cgnat: boolean;
    detail?: string;
    dns: DnsStatus;
    enabled: boolean;
    lan_ip?: string;
    lan_url?: string;
    last_check?: CheckResult;
    last_check_at?: string;
    observed_ip?: string;
    phase: string;
    port?: number;
    remote_url?: string;
    router_external_ip?: string;
    upnp: UPnPStatus;
};

export type RemoteStatusBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    available: boolean;
    config: RemoteConfigView;
    message?: string;
    status?: RemoteStatus;
};

export type ReorderListRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ReorderItem> | null;
};

export type ReorderItem = {
    media_item_id: number;
    sort_order: number;
};

export type Request = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    backend?: string;
    cfg?: number;
    device?: string;
    height?: number;
    memory_mode?: '' | 'auto' | 'low_vram';
    model_id?: string;
    negative_prompt?: string;
    prompt: string;
    seed?: number;
    steps?: number;
    width?: number;
};

export type RescueBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    rescued: number;
    retries_reset: number;
};

export type ResolveMatchRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Match candidate ID
     */
    candidate_id: number;
};

export type ScannerBucketCounts = {
    ignored: number;
    matched: number;
    needs_review: number;
    rejected: number;
    total: number;
    unmatched: number;
};

export type ScannerBulkApproveResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    approved: number;
};

export type ScannerBulkEligibleResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    eligible: number;
};

export type ScannerCandidateDetailView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    author?: string;
    backdrop_url?: string;
    candidate_id: number;
    description?: string;
    external_ids?: {
        [key: string]: string;
    };
    first_air_date?: string;
    genres?: Array<string> | null;
    heya_slug?: string;
    isbn?: string;
    language?: string;
    last_air_date?: string;
    networks?: Array<string> | null;
    number_of_episodes?: number;
    number_of_seasons?: number;
    page_count?: number;
    poster_url?: string;
    provider_id: string;
    provider_kind: string;
    provider_name: string;
    publish_date?: string;
    publisher?: string;
    runtime_minutes?: number;
    status?: string;
    subjects?: Array<string> | null;
    title: string;
    year?: string;
};

export type ScannerCandidateView = {
    author?: string;
    description?: string;
    external_ids?: {
        [key: string]: string;
    };
    heya_slug?: string;
    id: number;
    identity_id: number;
    identity_key: string;
    identity_title: string;
    identity_year?: string;
    poster_url?: string;
    provider_id: string;
    provider_kind: string;
    provider_name: string;
    rank: number;
    rejection_reason?: string;
    scan_run_id?: number;
    score?: number;
    status: string;
    title: string;
    year?: string;
};

export type ScannerFindingView = {
    code: string;
    created_at?: string;
    data: {
        [key: string]: unknown;
    };
    id: number;
    identity_id?: number;
    identity_key?: string;
    identity_title?: string;
    identity_year?: string;
    library_file_id?: number;
    library_id: number;
    media_item_id?: number;
    media_title?: string;
    media_type: string;
    message: string;
    rel_path?: string;
    scan_run_id?: number;
    severity: string;
};

export type ScannerIdentityView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    bucket: string;
    candidate_count: number;
    confidence: number;
    id: number;
    identity_key: string;
    last_seen_scan_run_id?: number;
    library_id: number;
    main_finding_code?: string;
    main_finding_message?: string;
    main_finding_severity?: string;
    media_item_id?: number;
    media_type: string;
    metadata_provider_id?: string;
    open_finding_count: number;
    review_status: string;
    selected_provider_id?: string;
    selected_score?: number;
    selected_title?: string;
    selected_year?: string;
    source: string;
    title: string;
    updated_at?: string;
    year?: string;
};

export type ScannerIssueCount = {
    code: string;
    count: number;
    severity: string;
};

export type ScannerOverview = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    bucket_counts: ScannerBucketCounts;
    issue_counts: Array<ScannerIssueCount> | null;
    issue_total: number;
    latest_run?: ScannerRunView;
    pipeline_failures: Array<ScannerPipelineFailureView> | null;
};

export type ScannerPipelineFailureView = {
    error_message: string;
    id: number;
    identity_key: string;
    stage: string;
    status: string;
    title: string;
    updated_at?: string;
};

export type ScannerRunView = {
    created_at?: string;
    error_message?: string;
    finished_at?: string;
    id: number;
    library_id: number;
    media_type: string;
    mode: string;
    pipeline_error_message?: string;
    pipeline_failure_count?: number;
    scanner_version: string;
    started_at?: string;
    status: string;
    summary: {
        [key: string]: unknown;
    };
};

export type SearchBucket = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: unknown;
    total: number;
};

export type SearchEvidence = {
    detail?: string;
    field: string;
    outcome: string;
    weight: number;
};

export type SearchPeopleByNameRow = {
    id: number;
    name: string;
    profile_path: string;
};

export type SearchProductionCompaniesByNameRow = {
    id: number;
    logo_path: string;
    name: string;
};

export type SearchResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    data: Array<SubtitleResult> | null;
    page: number;
    total_count: number;
    total_pages: number;
};

export type SearchResult = {
    alt_titles?: Array<string> | null;
    confidence: number;
    description: string;
    enriched?: boolean;
    evidence?: Array<SearchEvidence> | null;
    external_ids?: {
        [key: string]: string;
    };
    heya_slug?: string;
    poster_url: string;
    provider_id: string;
    provider_name: string;
    recommendation?: string;
    requires_review?: boolean;
    title: string;
    year: string;
};

export type SeasonWatchInfo = {
    episode_ids: Array<number> | null;
    season_id: number;
    total: number;
    watched: number;
};

export type SemanticSearchResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ForYouItem> | null;
    ml_ready: boolean;
};

export type Session = {
    album_title?: string;
    artist_name?: string;
    audio_codec?: string;
    bitrate_kbps?: number;
    client_ip?: string;
    client_user_agent?: string;
    container?: string;
    entity_id?: number;
    entity_type?: string;
    episode_number?: number;
    episode_title?: string;
    file_id: number;
    height?: number;
    last_heartbeat_at: string;
    media_item_id: number;
    media_subtitle?: string;
    media_title: string;
    media_type: string;
    paused: boolean;
    playback_action?: string;
    position_seconds: number;
    season_number?: number;
    session_id: string;
    started_at: string;
    total_seconds: number;
    user_id: number;
    username: string;
    video_codec?: string;
    width?: number;
};

export type SessionCommandInput = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    action: 'stop' | 'message';
    message?: string;
};

export type SessionHeartbeatInput = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    audio_codec?: string;
    bitrate_kbps?: number;
    client_ip?: string;
    client_user_agent?: string;
    container?: string;
    entity_id?: number;
    entity_type?: string;
    file_id?: string;
    height?: number;
    media_item_id: number;
    paused: boolean;
    playback_action?: string;
    position_seconds: number;
    session_id: string;
    total_seconds: number;
    video_codec?: string;
    width?: number;
};

export type SessionSnapshot = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    album?: string;
    artist?: string;
    audio_track?: number;
    device_id: string;
    device_name: string;
    duration_sec?: number;
    entity_id?: number;
    entity_type?: string;
    file_id?: string;
    id: string;
    media_item_id?: number;
    media_kind?: string;
    position_sec: number;
    quality?: string;
    state: string;
    subtitle_track?: number;
    title?: string;
    track_id?: number;
    updated_at: string;
    user_id: number;
    volume: number;
};

export type SessionsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<SessionSnapshot> | null;
};

export type SetCastConfigRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Regular users allowed to discover and control server-side cast receivers; admins are always allowed
     */
    allowed_user_ids: Array<number> | null;
    /**
     * Optional receiver-facing Heya origin for Chromecast/DLNA URL pulls; empty derives the routed LAN address
     */
    base_url: string;
    /**
     * Comma-separated receiver addresses resolved by unicast mDNS (same-subnet only)
     */
    devices: string;
    enabled: boolean;
};

export type SetPlaybackPreferenceRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * ISO 639-1/-2/-3 code or empty to clear
     */
    audio_language: string;
    /**
     * ISO 639-1/-2/-3 code or empty to clear
     */
    subtitle_language: string;
    /**
     * 'off' | 'forced' | 'full' | empty to clear
     */
    subtitle_mode: string;
};

export type SetPlaylistPinRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    pinned: boolean;
    /**
     * Which pin set to toggle
     */
    scope: 'page' | 'sidebar';
};

export type SetPlaylistSidebarOrderRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Playlist IDs in the desired top-to-bottom order (full list)
     */
    ids: Array<number> | null;
};

export type SetSystemSettingRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * JSON-encoded value
     */
    value: unknown;
};

export type SimilarAlbumsRow = {
    album_cover_path: string;
    album_slug: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    distance: number;
    id: number;
    title: string;
};

export type SimilarArtistRow = {
    image?: string;
    local_artist_id?: number;
    local_slug?: string;
    mbid?: string;
    name: string;
    score: number;
    source: string;
    url?: string;
};

export type SimilarArtistsRow = {
    distance: number;
    id: number;
    media_item_id: number;
    media_item_public_id: string;
    media_slug: string;
    name: string;
};

export type SimilarTracksByTextRichRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    distance: number;
    duration: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type SimilarTracksByTrackRichRow = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    distance: number;
    duration: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type SonicAnalysisSettings = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    accelerator: string;
    enabled: boolean;
};

export type SonicSaveBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Whether the new settings were live-applied or queued for next idle window
     */
    applied: boolean;
    status: string;
};

export type SourceEntry = {
    env_var?: string;
    source: string;
};

export type StaticTargetStatus = {
    addr: string;
    checked_at: string;
    device_id?: string;
    error?: string;
    name?: string;
    ok: boolean;
};

export type Station = {
    bitrate: number;
    clickcount: number;
    codec: string;
    country: string;
    countrycode: string;
    favicon: string;
    homepage: string;
    language: string;
    name: string;
    stationuuid: string;
    tags: string;
    url: string;
    url_resolved: string;
    votes: number;
};

export type StationInput = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    bitrate?: number;
    clickcount?: number;
    codec?: string;
    country?: string;
    countrycode?: string;
    favicon?: string;
    homepage?: string;
    language?: string;
    name: string;
    stationuuid: string;
    tags?: string;
    url?: string;
    url_resolved?: string;
    votes?: number;
};

export type StationResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    kind: string;
    label: string;
    tracks: Array<StationTrack> | null;
};

export type StationTrack = {
    album_cover_path: string;
    album_id: number;
    album_slug: string;
    album_title: string;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    track_id: number;
    track_number: number;
    track_title: string;
};

export type StationsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<Station> | null;
};

export type Status = {
    backend_state: string;
    cert_domain?: string;
    enabled: boolean;
    funnel: boolean;
    funnel_active: boolean;
    funnel_url?: string;
    hostname: string;
    https: boolean;
    https_active: boolean;
    https_url?: string;
    ipv4?: string;
    ipv6?: string;
    last_error?: string;
    login_url?: string;
    magic_dns?: string;
    running: boolean;
    updated_at: string;
};

export type StatusBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    status: string;
};

export type StatusOutputBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * Action status
     */
    status: string;
};

export type StreamInfoResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    audio: Array<AudioStream> | null;
    bit_rate: number;
    container: string;
    duration: number;
    library_id: number;
    playback: PlaybackDecision;
    qualities: Array<QualityOption> | null;
    size: number;
    subtitle: Array<SubStream> | null;
    video: Array<VideoStream> | null;
};

export type StudiosMediaIdsRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    company_ids: Array<number> | null;
};

export type SubFile = {
    file_id: number;
    file_name: string;
};

export type SubStream = {
    codec: string;
    delivery: string;
    index: number;
    is_default: boolean;
    is_forced: boolean;
    is_hearing_impaired: boolean;
    language: string;
    title?: string;
};

export type SubscribePodcastRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artwork_url: string;
    author: string;
    feed_url: string;
    title: string;
};

export type SubsonicConfigBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    enabled: boolean;
};

export type SubsonicCredentialBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    created_at: string;
    last_used_at?: string;
    rotated_at: string;
    secret: string;
};

export type SubtitleAttributes = {
    ai_translated: boolean;
    download_count: number;
    feature_details: FeatureDetails;
    files: Array<SubFile> | null;
    foreign_parts_only: boolean;
    fps: number;
    from_trusted: boolean;
    hd: boolean;
    hearing_impaired: boolean;
    language: string;
    machine_translated: boolean;
    new_download_count: number;
    ratings: number;
    release: string;
    subtitle_id: string;
    upload_date: string;
    uploader: Uploader;
    votes: number;
};

export type SubtitleResult = {
    attributes: SubtitleAttributes;
    id: string;
    type: string;
};

export type SubtitleTrack = {
    codec: string;
    delivery: string;
    index: number;
    is_default: boolean;
    is_forced: boolean;
    is_hearing_impaired: boolean;
    language: string;
    title: string;
};

export type SystemSettingBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    key: string;
    value?: unknown;
};

export type Tag = {
    name: string;
    stationcount: number;
};

export type TailscaleConfigPayload = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    enabled: boolean;
    funnel: boolean;
    hostname: string;
    https: boolean;
};

export type TailscaleStatusBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    config?: TailscaleConfigPayload;
    enabled: boolean;
    message?: string;
    status?: Status;
};

export type TaskItem = {
    detail?: string;
    error?: string;
    id: number;
    name: string;
    path: string;
    status: string;
};

export type TaskItemsResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    complete: number;
    failed: number;
    items: Array<TaskItem> | null;
    pending: number;
    total: number;
};

export type TaskResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    category: string;
    daily_end_time: string;
    daily_start_time: string;
    description: string;
    display_name: string;
    enabled: boolean;
    id: string;
    interval_hours: number;
    last_run_at: string | null;
    last_run_duration_sec: number;
    last_run_items_processed: number;
    last_run_items_total: number;
    last_run_result: string;
    max_runtime_minutes: number;
    next_run_at: string | null;
    runtime?: TaskRuntime;
    state: string;
    stats?: TaskStats;
};

export type TaskRuntime = {
    pending: number;
    running: number;
    state: string;
};

export type TaskStats = {
    complete: number;
    failed?: number;
    pending: number;
    total: number;
};

export type TempoBucket = {
    artists: Array<BrowseBucketArtist> | null;
    key: string;
    label: string;
    max_bpm: number;
    min_bpm: number;
    track_count: number;
};

export type TempoBucketsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<TempoBucket> | null;
};

export type Text = {
    String: string;
    Valid: boolean;
};

export type Timestamptz = {
    InfinityModifier: number;
    Time: string;
    Valid: boolean;
};

export type ToggleFavoriteRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    entity_id: number;
    /**
     * Entity kind
     */
    entity_type: 'media_item' | 'episode' | 'season' | 'track' | 'artist' | 'album';
};

export type ToggleTailscaleFunnelRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    enabled: boolean;
};

export type TopTracksBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<ArtistTopTrackRow> | null;
};

export type TopUserGenresRow = {
    genre_name: string;
    play_count: number;
};

export type TopUserMoodsRow = {
    avg_score: number;
    mood_key: string;
    sample_count: number;
};

export type TrackFile = {
    bit_depth: number;
    bitrate_kbps: number;
    boundaries_analyzed_at: Timestamptz;
    channels: number;
    created_at: Timestamptz;
    duration: number;
    fade_start_ms: Int4;
    format: string;
    id: number;
    integrated_lufs: Numeric;
    intro_end_ms: Int4;
    library_file_id: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    lyrics_path: string;
    outro_start_ms: Int4;
    quality_score: number;
    sample_peak_db: Numeric;
    sample_rate_hz: number;
    silence_start_ms: Int4;
    size_bytes: number;
    track_id: number;
    true_peak_db: Numeric;
};

export type TrackIdsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    /**
     * List of track IDs to look up
     */
    track_ids: Array<number> | null;
};

export type TrackResultsBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<SimilarTracksByTrackRichRow> | null;
};

export type TrackTextSearchBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<SimilarTracksByTextRichRow> | null;
};

export type TrackView = {
    album_id: number;
    artist_credits: string;
    credits?: Array<RecordingCredit> | null;
    disc_number: number;
    duration: number;
    explicit: boolean;
    external_ids: string;
    files: Array<TrackFile> | null;
    id: number;
    isrc: string;
    lyrics_available: boolean;
    preview_url: string;
    recording_mbid: string;
    search_vector: unknown;
    sort_album: string;
    sort_album_year: string;
    sort_artist: string;
    title: string;
    track_number: number;
};

export type TranscodeProgressResponse = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    active: boolean;
    bitrate_kbps: number;
    drop_frames: number;
    dup_frames: number;
    elapsed_seconds: number;
    fps: number;
    frame: number;
    head_current_segment: number;
    head_start_segment: number;
    head_stop_reason?: string;
    last_requested_segment: number;
    last_update_ago_ms: number;
    lead_cap_seconds: number;
    out_time_seconds: number;
    ready_segments: number;
    running: boolean;
    session_key?: string;
    speed: number;
    started_at_unix_ms?: number;
    state: string;
    total_segments: number;
    total_size_bytes: number;
    updated_at_unix_ms?: number;
};

export type TranscodeStatusBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    active_jobs: number;
    available: boolean;
    cache_dir: string;
    cache_items: number;
    cache_max_gb: number;
    cache_size_mb: number;
    config_mode: string;
    encoder_h264: string;
    encoder_hevc: string;
    hw_accel: string;
    hw_accel_label: string;
};

export type TvEpisode = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    absolute_number: number;
    air_date: Date;
    episode_number: number;
    episode_type: number;
    external_ids: string;
    id: number;
    is_special: boolean;
    overview: string;
    rating: Numeric;
    runtime_minutes: number;
    season_id: number;
    source: string;
    still_path: string;
    title: string;
};

export type TvSeason = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    air_date: Date;
    aired_episodes: number;
    end_date: Date;
    external_ids: string;
    id: number;
    overview: string;
    poster_path: string;
    season_number: number;
    series_id: number;
    status: string;
    title: string;
};

export type UiSettings = {
    pinned_hero_mode?: string;
};

export type UPnPStatus = {
    available: boolean;
    error?: string;
    gateway?: string;
    mapped_at?: string;
    mappings?: Array<PortMappingStatus> | null;
};

export type UnmatchedFile = {
    candidates: Array<MatchCandidate> | null;
    file: LibraryFile;
};

export type UpNextRailItem = {
    episode_id: number;
    episode_number: number;
    episode_title?: string;
    file_id: number;
    file_public_id: string;
    last_watched_at: string;
    media_item_id: number;
    media_item_public_id: string;
    media_type: string;
    runtime?: number;
    season_id: number;
    season_number: number;
    slug: string;
    title: string;
};

export type UpNextResult = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    episode_id?: number;
    episode_number?: number;
    episode_title?: string;
    file_id?: number;
    file_public_id?: string;
    has_next: boolean;
    media_item_id?: number;
    media_item_public_id?: string;
    runtime?: number;
    season_id?: number;
    season_number?: number;
};

export type UpdateLibraryRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    name: string;
    /**
     * Absolute filesystem directory paths visible to the Heya host or container; mount network shares before configuring them
     */
    paths: Array<string> | null;
};

export type UpdateTaskRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    daily_end_time: string;
    /**
     * HH:MM 24h or empty
     */
    daily_start_time: string;
    enabled: boolean;
    interval_hours: number;
    max_runtime_minutes: number;
};

export type UpdateTranscodeSettingsRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    cache_max_gb: number;
    hw_accel: 'auto' | 'none' | 'vaapi' | 'qsv' | 'nvenc' | 'videotoolbox';
};

export type UpdateUserListRequest = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    description: string;
    filter_json: unknown;
    icon: string;
    name: string;
};

export type UpdateAlbumReq = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    album_type?: string;
    barcode?: string;
    country?: string;
    genres?: Array<string> | null;
    label?: string;
    release_date?: string;
    title?: string;
    year?: string;
};

export type UpdateEpisodeReq = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    air_date?: string;
    overview?: string;
    runtime_minutes?: number;
    title?: string;
};

export type UpdateMediaMetadataReq = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    author_name?: string;
    biography?: string;
    description?: string;
    disambiguation?: string;
    external_ids?: {
        [key: string]: string;
    };
    first_air_date?: string;
    format?: string;
    genres?: Array<string> | null;
    isbn?: string;
    language?: string;
    last_air_date?: string;
    networks?: Array<string> | null;
    original_language?: string;
    original_name?: string;
    original_title?: string;
    page_count?: number;
    publish_date?: string;
    publisher?: string;
    release_date?: string;
    runtime_minutes?: number;
    series_name?: string;
    series_number?: number;
    sort_name?: string;
    sort_title?: string;
    status?: string;
    subjects?: Array<string> | null;
    tagline?: string;
    title?: string;
    year?: string;
};

export type UpdateSeasonReq = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    air_date?: string;
    overview?: string;
    title?: string;
};

export type UploadAssetResultBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    asset?: MediaAsset;
    path?: string;
    status: string;
};

export type Uploader = {
    name: string;
    rank: string;
    uploader_id: number;
};

export type UserInfo = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    allowed_downloads: number;
    allowed_translations: number;
    ext_installed: boolean;
    level: string;
    remaining_downloads: number;
    user_id: number;
    vip: boolean;
};

export type UserList = {
    created_at: Timestamptz;
    description: string;
    filter_json: string;
    icon: string;
    id: number;
    list_type: string;
    media_type: string;
    name: string;
    updated_at: Timestamptz;
    user_id: number;
};

export type UserListDetailBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    items: Array<MediaItemCard> | null;
    list: UserList;
};

export type UserListItem = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    added_at: Timestamptz;
    id: number;
    list_id: number;
    media_item_id: number;
    sort_order: number;
};

export type UserListView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    contains?: boolean;
    created_at: Timestamptz;
    description: string;
    filter_json: unknown;
    icon: string;
    id: number;
    item_count: number;
    list_type: string;
    media_type: string;
    name: string;
    updated_at: Timestamptz;
    user_id: number;
};

export type UserPlaylist = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    cover_path: string;
    created_at: Timestamptz;
    description: string;
    id: number;
    name: string;
    pinned: boolean;
    sidebar_pinned: boolean;
    sidebar_position: number;
    slug: string;
    tags: Array<string> | null;
    updated_at: Timestamptz;
    user_id: number;
};

export type UserPodcastProgress = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artwork_url: string;
    audio_url: string;
    completed: boolean;
    episode_guid: string;
    feed_url: string;
    id: number;
    progress_seconds: number;
    title: string;
    total_seconds: number;
    updated_at: Timestamptz;
    user_id: number;
};

export type UserPodcastSubscription = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    artwork_url: string;
    author: string;
    created_at: Timestamptz;
    feed_url: string;
    id: number;
    last_episode_at: Timestamptz;
    title: string;
    user_id: number;
};

export type UserRadioFavorite = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    bitrate: number;
    codec: string;
    country: string;
    countrycode: string;
    created_at: Timestamptz;
    favicon: string;
    homepage: string;
    id: number;
    language: string;
    name: string;
    stationuuid: string;
    tags: string;
    url: string;
    user_id: number;
};

export type UserSettings = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    appearance: AppearanceSettings;
    home: HomeSettings;
    playback: PlaybackSettings;
    ui: UiSettings;
};

export type UserTempoHistogramRow = {
    band: string;
    play_count: number;
};

export type UserView = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    email: string;
    id: number;
    is_admin: boolean;
    username: string;
};

export type VideoStream = {
    bit_rate?: string;
    codec: string;
    codec_long: string;
    color_primaries?: string;
    color_space?: string;
    color_transfer?: string;
    hdr: boolean;
    height: number;
    index: number;
    is_default: boolean;
    pix_fmt?: string;
    profile?: string;
    width: number;
};

export type WatchedBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    watched: boolean;
};

export type WatcherEntry = {
    library_id: number;
    path: string;
};

export type WatcherStatusBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    count: number;
    /**
     * Time of the most recent worker heartbeat
     */
    updated_at?: string;
    watchers: Array<WatcherEntry> | null;
    /**
     * Whether the dedicated worker heartbeat is current
     */
    worker_online: boolean;
};

export type WaveformBody = {
    /**
     * A URL to the JSON Schema for this object.
     */
    readonly $schema?: string;
    waveform: unknown;
};

export type WorkerRuntimeStatus = {
    cpu_percent: number;
    gomaxprocs: number;
    goroutines: number;
    heap_alloc_bytes: number;
    heap_inuse_bytes: number;
    heartbeat_at: string;
    host_cpu_available: boolean;
    host_cpu_metric: string;
    host_cpu_percent: number;
    hostname?: string;
    log_level: string;
    num_cpu: number;
    pid?: number;
    running: boolean;
    started_at: string;
    sys_bytes: number;
    watchers: Array<WorkerRuntimeWatcher> | null;
};

export type WorkerRuntimeWatcher = {
    library_id: number;
    path: string;
};

export type AiChatRequestWritable = {
    max_tokens?: number;
    /**
     * full message history; overrides prompt/system
     */
    messages?: Array<Message> | null;
    prompt?: string;
    /**
     * optional system prompt / context
     */
    system?: string;
};

export type AiChatResponseWritable = {
    completion_tokens: number;
    content: string;
    duration_ms: number;
    mode: string;
    model?: string;
    prompt_tokens: number;
};

export type AiMusicMixRequestWritable = {
    /**
     * Number of tracks (default 30)
     */
    limit?: number;
    /**
     * Narrative description of the desired mix
     */
    query: string;
};

export type AiMusicMixResultWritable = {
    duration_ms: number;
    mode: string;
    model?: string;
    /**
     * Acoustic CLAP searches derived from the brief
     */
    probes: Array<string> | null;
    summary: string;
    title: string;
    tracks: Array<AiMusicMixTrack> | null;
};

export type AiRecommendRequestWritable = {
    limit?: number;
    query: string;
    type?: string;
};

export type AiRecommendResultWritable = {
    duration_ms: number;
    items: Array<ForYouItem> | null;
    mode: string;
    model?: string;
    /**
     * the model's overall explanation of how it read the ask and why the picks fit
     */
    note?: string;
    /**
     * embedding probes the model searched with
     */
    probes?: Array<string> | null;
};

export type AiSettingsWritable = {
    api_key: string;
    base_url: string;
    claude_model: string;
    claude_token: string;
    codex_model: string;
    context_size: number;
    local_backend: string;
    local_model: string;
    mode: string;
    model: string;
    provider: string;
};

export type AiSettingsViewWritable = {
    /**
     * last 4 characters, for recognition only
     */
    api_key_hint?: string;
    api_key_set: boolean;
    base_url: string;
    claude_model: string;
    /**
     * last 4 characters, for recognition only
     */
    claude_token_hint?: string;
    claude_token_set: boolean;
    codex_model: string;
    context_size: number;
    local_backend: string;
    local_model: string;
    mode: string;
    model: string;
    provider: string;
};

export type AiStatusReportWritable = {
    agent: AiAgentStatus;
    context_size?: number;
    /**
     * human-readable reason when not ready
     */
    detail?: string;
    local: AiLocalStatus;
    local_model?: string;
    mode: string;
    model?: string;
    provider?: string;
    ready: boolean;
};

export type ActiveSessionsBodyWritable = {
    items: Array<Session> | null;
};

export type AddListItemRequestWritable = {
    media_item_id: number;
};

export type AddedBodyWritable = {
    added: number;
};

export type AdminCreateUserRequestWritable = {
    email: string;
    is_admin: boolean;
    password: string;
    username: string;
};

export type AdminResetUserPasswordRequestWritable = {
    new_password: string;
};

export type AdminSetLogLevelRequestWritable = {
    /**
     * New zerolog level
     */
    level: 'trace' | 'debug' | 'info' | 'warn' | 'error' | 'fatal' | 'panic' | 'disabled';
};

export type AdminSetUserRoleRequestWritable = {
    is_admin: boolean;
};

export type AdminStorageScanRequestWritable = {
    /**
     * Scan a single library; omit to scan all
     */
    library_id?: number;
};

export type AdminDbBodyWritable = {
    acquire_count: number;
    acquire_duration_ms: number;
    acquired_connections: number;
    active_queries: number;
    blocks_hit: number;
    blocks_read: number;
    buffer_cache_hit_ratio: number;
    canceled_acquire_count: number;
    database_name: string;
    dead_tuples: number;
    deadlocks: number;
    empty_acquire_count: number;
    error?: string;
    idle_connections: number;
    index_scan_ratio: number;
    longest_query_ms: number;
    max_connections: number;
    query_stats_available: boolean;
    query_stats_error?: string;
    rows_deleted: number;
    rows_fetched: number;
    rows_inserted: number;
    rows_returned: number;
    rows_updated: number;
    size_bytes: number;
    temp_bytes: number;
    top_queries: Array<AdminDbQuery> | null;
    top_tables: Array<AdminDbTable> | null;
    total_connections: number;
    transactions_committed: number;
    transactions_rolled_back: number;
    version: string;
    waiting_queries: number;
};

export type AdminDiagnosticsBodyWritable = {
    database: AdminDbBodyWritable;
    findings: Array<AdminDiagnosticFinding> | null;
    generated_at: string;
    http: HttpMetrics;
    http_available: boolean;
    logs: AdminLogSummary;
    queries: QuerySnapshot;
    status: 'healthy' | 'watching' | 'degraded';
    system: AdminSystemBodyWritable;
    worker: WorkerRuntimeStatus;
    worker_online: boolean;
};

export type AdminListenersBodyWritable = {
    listeners: Array<AdminListener> | null;
    ws_subscribers: number;
};

export type AdminLogLevelBodyWritable = {
    available: Array<string> | null;
    /**
     * Level loaded from HEYA_LOG_LEVEL at boot
     */
    boot_level: string;
    level: string;
};

export type AdminNetworkStatusBodyWritable = {
    general: AdminNetworkGeneral;
    ingress: IngressStatus;
    remote?: RemoteStatus;
    tailscale?: Status;
    updated_at: string;
};

export type AdminStorageBodyWritable = {
    data_dir: string;
    data_dir_volume: AdminStoragePath;
    /**
     * Cached results from the last scan_library_disk run; empty until a scan completes
     */
    library_disk_usage: Array<LibraryDiskUsage> | null;
    library_paths: Array<AdminStoragePath> | null;
    transcode_dir: string;
    transcode_items: number;
    transcode_max_gb: number;
    transcode_used_mb: number;
    transcode_volume: AdminStoragePath;
};

export type AdminSystemBodyWritable = {
    build?: {
        [key: string]: unknown;
    };
    /**
     * Serve process CPU where one fully occupied logical core equals 100 percent
     */
    cpu_percent: number;
    gc_pause_last_ns: number;
    go_version: string;
    goarch: string;
    gomaxprocs: number;
    goos: string;
    goroutines: number;
    heap_alloc_bytes: number;
    heap_inuse_bytes: number;
    /**
     * Whether the host exposes a readable CPU counter
     */
    host_cpu_available: boolean;
    /**
     * cpu_utilization on Linux or load_average_1m on macOS
     */
    host_cpu_metric: string;
    /**
     * Whole-host load as a percentage of logical CPU capacity
     */
    host_cpu_percent: number;
    hostname: string;
    num_cgo_call: number;
    num_cpu: number;
    num_gc: number;
    pid: number;
    stack_bytes: number;
    started_at: string;
    sys_bytes: number;
    uptime_seconds: number;
    ws_subscribers: number;
};

export type AdminUserViewWritable = {
    created_at: string;
    email: string;
    id: number;
    is_admin: boolean;
    username: string;
};

export type AdminWorkersBodyWritable = {
    active_jobs: Array<JobRow> | null;
    error?: string;
    generated_at: string;
    online: boolean;
    queue_summary: Array<JobSummaryRow> | null;
    recent_jobs: Array<JobRow> | null;
    status: WorkerRuntimeStatus;
};

export type AiCatalogBodyWritable = {
    local_models: Array<LocalModel> | null;
    providers: Array<Provider> | null;
};

export type AiModelsBodyWritable = {
    models: Array<string> | null;
};

export type AiReadyBodyWritable = {
    mode: string;
    ready: boolean;
};

export type AlbumWritable = {
    album_type: string;
    artist_credits: string;
    artist_id: number;
    artwork: string;
    barcode: string;
    catalog_no: string;
    country: string;
    cover_path: string;
    description: string;
    duration_seconds: number;
    editions: string;
    explicit: boolean;
    external_ids: string;
    field_provenance: string;
    genres: Array<string> | null;
    id: number;
    integrated_lufs: Numeric;
    isrcs: Array<string> | null;
    label: string;
    language: string;
    listeners: number;
    loudness_analyzed_at: Timestamptz;
    loudness_range_db: Numeric;
    musicbrainz_id: string;
    original_title: string;
    playcount: number;
    popularity: number;
    rating: Numeric;
    ratings: string;
    release_date: Date;
    release_events: string;
    review: string;
    sales: number;
    script: string;
    search_vector: unknown;
    secondary_types: Array<string> | null;
    slug: string;
    sort_artist: string;
    sort_title: string;
    styles: Array<string> | null;
    tags: Array<string> | null;
    title: string;
    total_discs: number;
    total_tracks: number;
    true_peak_db: Numeric;
    year: string;
};

export type AlbumIdsBodyWritable = {
    /**
     * List of album IDs to look up
     */
    album_ids: Array<number> | null;
};

export type AlbumResultsBodyWritable = {
    items: Array<SimilarAlbumsRow> | null;
};

export type ApplyAlbumIdentifyRequestWritable = {
    provider_id: string;
    provider_name: string;
};

export type ApplyIdentifyRequestWritable = {
    provider_id: string;
    provider_name: string;
};

export type ArtistIdsBodyWritable = {
    /**
     * List of artist IDs to look up
     */
    artist_ids: Array<number> | null;
};

export type ArtistPlayQueueBodyWritable = {
    items: Array<ListArtistTracksTopPlayedFirstRow> | null;
};

export type ArtistResultsBodyWritable = {
    items: Array<SimilarArtistsRow> | null;
};

export type ArtworkBodyWritable = {
    results: unknown;
};

export type AuthBodyWritable = {
    /**
     * Session token
     */
    token: string;
    user: UserViewWritable;
};

export type BatchRatingsBodyWritable = {
    /**
     * Map of track_id (as string) → rating 1..10. Tracks the user hasn't rated are omitted entirely.
     */
    ratings: {
        [key: string]: number;
    };
};

export type CancelBodyWritable = {
    cancelled: number;
    status: string;
};

export type CastPlayRequestWritable = {
    /**
     * Zero-based audio-stream selection for video
     */
    audio_track?: number;
    /**
     * Target device (from /api/cast/devices)
     */
    device_id: string;
    /**
     * Movie media-item ID or TV episode ID for video progress
     */
    entity_id?: number;
    /**
     * Watch-progress entity type for video
     */
    entity_type?: 'movie' | 'episode';
    /**
     * Video library-file reference; mutually exclusive with track_id
     */
    file_id?: string;
    /**
     * Optional HLS quality profile for video; auto uses the source-compatible plan
     */
    quality?: string;
    /**
     * Load video paused; used when changing remote track options while paused
     */
    start_paused?: boolean;
    /**
     * Start position in the media item — lets a client hand off mid-playback
     */
    start_seconds?: number;
    /**
     * Zero-based text-subtitle selection for video; omit for subtitles off
     */
    subtitle_track?: number;
    /**
     * Display title for video playback
     */
    title?: string;
    /**
     * Music track to play; mutually exclusive with file_id
     */
    track_id?: number;
    /**
     * Initial device volume (ignored when retargeting an existing session)
     */
    volume: number;
};

export type CastSeekRequestWritable = {
    /**
     * Absolute position in the track
     */
    seconds: number;
};

export type CastVolumeRequestWritable = {
    /**
     * Device stream volume
     */
    level: number;
};

export type CastConfigViewWritable = {
    allowed_user_ids: Array<number> | null;
    base_url: string;
    base_url_source: string;
    devices: string;
    devices_source: string;
    enabled: boolean;
    enabled_source: string;
};

export type CastNetworkStatusWritable = {
    devices: Array<Device> | null;
    enabled: boolean;
    interfaces: Array<CastInterface> | null;
    running: boolean;
    sessions: Array<SessionSnapshotWritable> | null;
    static: Array<StaticTargetStatus> | null;
};

export type ChangePasswordRequestWritable = {
    /**
     * Current password (verified before swap)
     */
    current_password: string;
    /**
     * New password — minimum 8 chars
     */
    new_password: string;
};

export type ClearedBodyWritable = {
    cleared: number;
};

export type CollectionListResultWritable = {
    items: Array<ListAllCollectionsRow> | null;
    total: number;
};

export type CollectionResultWritable = {
    collection: Collection;
    genres: Array<string> | null;
    keywords: Array<string> | null;
    movies: Array<MediaItemCard> | null;
    owned_count: number;
    parts: Array<CollectionPartView> | null;
};

export type CreateApiTokenRequestWritable = {
    /**
     * 0 means never expires
     */
    expires_in_days: number;
    /**
     * Human label so you can recognise the token
     */
    name: string;
};

export type CreateNativePlaybackGrantRequestWritable = {
    audio_track?: number;
    file_id: string;
    /**
     * direct or hls; defaults to direct
     */
    mode?: string;
    quality?: string;
};

export type CreateUserListRequestWritable = {
    description: string;
    /**
     * Smart-list filter spec, ignored for manual
     */
    filter_json: unknown;
    /**
     * manual (user-curated) or smart (filter-backed)
     */
    list_type: 'manual' | 'smart';
    media_type: 'movie' | 'tv' | 'music' | 'book' | 'comic' | 'podcast' | 'radio';
    name: string;
};

export type CreateApiTokenResultWritable = {
    created_at: string;
    expires_at?: string;
    id: number;
    last_seen_at: string;
    name: string;
    token: string;
};

export type CreateLibraryInputBodyWritable = {
    media_type: 'movie' | 'tv' | 'anime' | 'music' | 'book' | 'comic' | 'podcast' | 'radio';
    name: string;
    /**
     * Absolute filesystem directory paths visible to the Heya host or container; mount network shares before configuring them
     */
    paths: Array<string> | null;
    settings?: LibrarySettingsWritable;
};

export type DashboardStatsWritable = {
    libraries: number;
    media_counts: {
        [key: string]: number;
    };
    missing_count: number;
    queue_pending: number;
    queue_running: number;
    total_files: number;
    total_media: number;
    total_people: number;
};

export type DeletedCountBodyWritable = {
    deleted: number;
};

export type DevicesBodyWritable = {
    items: Array<Device> | null;
};

export type DoctorReportWritable = {
    app: DoctorAppSection;
    config: DoctorConfigSection;
    database: DoctorDatabaseSection;
    generated_at: string;
    libraries: DoctorLibrariesSection;
    logs: DoctorLogsSection;
    queue: DoctorQueueSection;
    storage: DoctorStorageSection;
    tools: DoctorToolsSection;
};

export type DownloadAssetRequestWritable = {
    asset_type: 'poster' | 'backdrop' | 'logo' | 'art' | 'clearart' | 'banner' | 'thumb' | 'disc' | 'still';
    label?: string;
    url: string;
};

export type EnrichedMediaBodyWritable = {
    movies?: Array<EnrichedMovieView> | null;
    tv?: Array<EnrichedTvView> | null;
    /**
     * Echoes the requested ?type=
     */
    type: 'movie' | 'tv';
};

export type ErrorModelWritable = {
    /**
     * A human-readable explanation specific to this occurrence of the problem.
     */
    detail?: string;
    /**
     * Optional list of individual error details
     */
    errors?: Array<ErrorDetail> | null;
    /**
     * A URI reference that identifies the specific occurrence of the problem.
     */
    instance?: string;
    /**
     * HTTP status code
     */
    status?: number;
    /**
     * A short, human-readable summary of the problem type. This value should not change between occurrences of the error.
     */
    title?: string;
    /**
     * A URI reference to human-readable documentation for the error.
     */
    type?: string;
};

export type ExternalPlaylistSyncBodyWritable = {
    enabled: boolean;
    playlist_id?: number;
};

export type FacetsViewWritable = {
    analyzed_at?: string;
    analyzer_version: number;
    bpm?: number;
    bpm_confidence?: number;
    key?: KeyView;
    mood_tags?: {
        [key: string]: number;
    };
    top_genres?: Array<GenreScore> | null;
    track_id: number;
};

export type FavoritedBodyWritable = {
    favorited: boolean;
};

export type FileSegmentsResponseWritable = {
    segments: Array<FileSegment> | null;
};

export type ForYouResultWritable = {
    acquire?: Array<AcquireItem> | null;
    has_signal: boolean;
    items: Array<ForYouItem> | null;
};

export type FsBrowseBodyWritable = {
    entries: Array<FsEntry> | null;
    parent?: string;
    path: string;
};

export type FunnelBodyWritable = {
    funnel: boolean;
};

export type GenreBucketsBodyWritable = {
    items: Array<GenreBucket> | null;
};

export type GenreResultWritable = {
    genre: string;
    items: Array<MediaItemCard> | null;
    total: number;
    type_counts?: {
        [key: string]: number;
    };
};

export type GetUserStateRequestWritable = {
    scope: 'movies' | 'series' | 'seasons' | 'episodes';
    series_id?: number;
};

export type GetMusicArtistBySlugRowWritable = {
    album_count: number;
    aliases: Array<string> | null;
    annotation: string;
    artist_type: string;
    available: boolean;
    begin_date: string;
    begin_year: number;
    biography: string;
    birthplace: string;
    cover_art_enriched_at: Timestamptz;
    deathday: string;
    disambiguation: string;
    discography_enriched_at: Timestamptz;
    end_date: string;
    ended: boolean;
    followers: number;
    genres: Array<string> | null;
    groups: string;
    id: number;
    listeners: number;
    media_item_id: number;
    media_item_public_id: string;
    members: string;
    metadata_sources: Array<string> | null;
    musicbrainz_id: string;
    name: string;
    playcount: number;
    popularity: number;
    poster_path: string;
    profiles: string;
    search_vector: unknown;
    slug: string;
    sort_name: string;
    tags: Array<string> | null;
    track_count: number;
    urls: string;
    wikipedia_links: string;
};

export type GetUserRatedTracksStatsRowWritable = {
    artist_count: number;
    last_rated_at: Timestamptz;
    total_duration: number;
    track_count: number;
};

export type HealthBodyWritable = {
    /**
     * Database connection status
     */
    database: string;
    /**
     * Server status
     */
    status: string;
    /**
     * Build version (overridden at link time)
     */
    version: string;
};

export type IdentifyBodyWritable = {
    results: unknown;
};

export type IdentifySearchResultWritable = {
    results: Array<SearchResult> | null;
};

export type IdsBodyWritable = {
    ids: Array<number> | null;
};

export type ImageCatalogBodyWritable = {
    models: Array<Model> | null;
};

export type ImageFetchBodyWritable = {
    backend?: string;
    model?: string;
};

export type ImageGenerateBodyWritable = {
    duration_ms: number;
    model: string;
    seed: number;
    url: string;
};

export type ImageStatusWritable = {
    artifacts: Array<ArtifactStatus> | null;
    backend: string;
    build: string;
    device_error?: string;
    devices: Array<ComputeDevice> | null;
    download_bytes: number;
    download_error?: string;
    download_state: string;
    model: string;
    model_present: boolean;
    progress?: ImageDownloadProgress;
    runtime_present: boolean;
};

export type JellyfinConfigBodyWritable = {
    enabled: boolean;
};

export type JellyfinCredentialBodyWritable = {
    created_at: string;
    last_used_at?: string;
    pin: string;
    rotated_at: string;
};

export type JobListResultWritable = {
    has_more: boolean;
    jobs: Array<JobRow> | null;
    next_before_id?: number;
    total: number;
};

export type JobWorkerSettingsWritable = {
    restart_required: boolean;
    workers: Array<JobWorkerSetting> | null;
};

export type JobWorkerUpdateWritable = {
    workers: {
        [key: string]: number;
    };
};

export type KeywordResultWritable = {
    items: Array<MediaItemCard> | null;
    keyword: string;
    total: number;
    type_counts?: {
        [key: string]: number;
    };
};

export type LapsedShelfBodyWritable = {
    artists: Array<LapsedArtistEntry> | null;
    enabled: boolean;
    since_label: string;
};

export type LastfmAuthCompleteRequestWritable = {
    token: string;
};

export type LastfmAuthStartBodyWritable = {
    auth_url: string;
    token: string;
};

export type LibraryScannerApproveCandidateRequestWritable = {
    candidate_id: number;
};

export type LibraryScannerAssignIdentityRequestWritable = {
    confidence?: number;
    description?: string;
    external_ids?: {
        [key: string]: string;
    };
    heya_slug?: string;
    poster_url?: string;
    provider_id: string;
    provider_name?: string;
    title?: string;
    year?: string;
};

export type LibraryScannerBulkApproveSingleRequestWritable = {
    min_confidence: number;
};

export type LibraryScannerIgnoreIdentityRequestWritable = {
    reason?: string;
};

export type LibraryScannerRejectIdentityRequestWritable = {
    reason?: string;
};

export type LibrarySettingsWritable = {
    auto_collections: boolean;
    enable_trickplay: boolean;
    fetch_ratings: boolean;
    generate_thumbnails: boolean;
    match_threshold?: number;
    preferred_country: string;
    preferred_language: string;
    save_images: boolean;
    save_nfo: boolean;
    use_local_data: boolean;
    watch: boolean;
};

export type LibrarySettingsBodyWritable = {
    defaults: LibrarySettingsWritable;
    settings: LibrarySettingsWritable;
};

export type LibraryViewWritable = {
    created_by: number;
    id: number;
    media_type: string;
    name: string;
    paths: Array<string> | null;
    settings: LibrarySettingsWritable;
    sources: LibraryViewSources;
};

export type ListeningStatsWritable = {
    mood_avg: Array<TopUserMoodsRow> | null;
    tempo_histogram: Array<UserTempoHistogramRow> | null;
    top_genres: Array<TopUserGenresRow> | null;
    total_plays: number;
};

export type LiveBodyWritable = {
    /**
     * Always 'ok' when the process is alive
     */
    status: string;
};

export type LoginInputBodyWritable = {
    /**
     * Password
     */
    password: string;
    /**
     * Username
     */
    username: string;
};

export type LovedBodyWritable = {
    loved: boolean;
};

export type LyricsResponseWritable = {
    lines: Array<LyricsLine> | null;
    synced: boolean;
};

export type MarkMediaWatchedRequestWritable = {
    watched: boolean;
};

export type MarkSeasonWatchedRequestWritable = {
    watched: boolean;
};

export type MediaLanguagesWritable = {
    audio_languages: Array<LanguageInfo> | null;
    subtitle_languages: Array<LanguageInfo> | null;
};

export type MediaStateBodyWritable = {
    favorited: Array<number> | null;
    watched: Array<number> | null;
};

export type MetadataQueueStatusWritable = {
    pending: number;
    pending_by_priority: {
        [key: string]: number;
    };
    recent: MetadataQueueRecent;
    running?: MetadataQueueRunning;
};

export type MixToBodyWritable = {
    items: Array<MixToTracksRow> | null;
};

export type MixesBodyWritable = {
    items: Array<MusicMix> | null;
};

export type MoodBucketsBodyWritable = {
    items: Array<MoodBucket> | null;
};

export type MoreByArtistsBodyWritable = {
    items: Array<MoreByArtist> | null;
};

export type MoreFromLabelBodyWritable = {
    albums: Array<ListAlbumsByLabelRow> | null;
    enabled: boolean;
    label: string;
};

export type MoreInGenreBodyWritable = {
    artists: Array<ListArtistsByGenreRow> | null;
    enabled: boolean;
    genre: string;
};

export type MostPlayedBodyWritable = {
    albums: Array<MostPlayedAlbumsInRangeRow> | null;
    enabled: boolean;
    window_label: string;
};

export type MusicAlbumDetailWritable = {
    album: AlbumWritable;
    artist: ArtistView;
    artist_slug: string;
    artwork?: Array<AlbumArtworkRef> | null;
    editions?: Array<AlbumEdition> | null;
    media_item_id: number;
    media_item_public_id?: string;
    ratings?: Array<AlbumRating> | null;
    release_events?: Array<AlbumReleaseEvent> | null;
    tracks: Array<TrackView> | null;
};

export type MusicCountsWritable = {
    albums: number;
    artists: number;
    tracks: number;
};

export type MusicHomeDataWritable = {
    recent_albums: Array<ListRecentlyAddedAlbumsRow> | null;
    recent_artists: Array<RecentArtistEntry> | null;
};

export type MusicListPageListAlbumsByArtistSlugRowWritable = {
    items: Array<ListAlbumsByArtistSlugRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListMusicAlbumsRowWritable = {
    items: Array<ListMusicAlbumsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListMusicArtistsRowWritable = {
    items: Array<ListMusicArtistsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListMusicTracksRowWritable = {
    items: Array<ListMusicTracksRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByArtistSlugRowWritable = {
    items: Array<ListTracksByArtistSlugRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByGenreRowWritable = {
    items: Array<ListTracksByGenreRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByMoodRowWritable = {
    items: Array<ListTracksByMoodRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListTracksByTempoBandRowWritable = {
    items: Array<ListTracksByTempoBandRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserLovedAlbumsRowWritable = {
    items: Array<ListUserLovedAlbumsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserLovedArtistsRowWritable = {
    items: Array<ListUserLovedArtistsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserLovedTracksRowWritable = {
    items: Array<ListUserLovedTracksRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserRatedAlbumsRowWritable = {
    items: Array<ListUserRatedAlbumsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserRatedArtistsRowWritable = {
    items: Array<ListUserRatedArtistsRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicListPageListUserRatedTracksRowWritable = {
    items: Array<ListUserRatedTracksRow> | null;
    limit: number;
    offset: number;
    total: number;
};

export type MusicServiceUpdateWritable = {
    scrobble_enabled?: boolean;
    /**
     * ListenBrainz user token; empty keeps the stored one
     */
    token?: string;
    username?: string;
};

export type MusicServiceViewWritable = {
    import_state: unknown;
    scrobble_enabled: boolean;
    service: 'listenbrainz' | 'lastfm';
    token_set: boolean;
    username: string;
};

export type MusicServicesBodyWritable = {
    services: Array<MusicServiceViewWritable> | null;
};

export type MusicTrackDetailWritable = {
    album_cover_path: string;
    album_id: number;
    album_integrated_lufs: Numeric;
    album_slug: string;
    album_title: string;
    album_true_peak_db: Numeric;
    album_year: string;
    artist_id: number;
    artist_name: string;
    artist_slug: string;
    disc_number: number;
    duration: number;
    explicit: boolean;
    file_path: string;
    files: Array<TrackFile> | null;
    id: number;
    isrc: string;
    lyrics_available: boolean;
    lyrics_path: string;
    recording_mbid: string;
    title: string;
    track_number: number;
};

export type NativePlaybackGrantBodyWritable = {
    expires_at_unix_millis: number;
    header_name: string;
    media_path: string;
    playback_grant: string;
};

export type OkBodyWritable = {
    ok: boolean;
};

export type OnThisDayBodyWritable = {
    items: Array<ListOnThisDayAlbumsRow> | null;
};

export type OpensubtitlesDownloadRequestWritable = {
    file_id: number;
    file_name: string;
    language: string;
    media_item_id: number;
};

export type OsCredentialsWritable = {
    /**
     * OpenSubtitles API key
     */
    api_key: string;
    password: string;
    username: string;
};

export type OsDownloadBodyWritable = {
    asset: MediaAsset;
    remaining: number;
    status: string;
};

export type OsTestBodyWritable = {
    error?: string;
    ok: boolean;
    user?: unknown;
};

export type PeopleMediaIdsRequestWritable = {
    person_ids: Array<number> | null;
};

export type PlaybackEventWritable = {
    /**
     * Whether playback reached its natural end
     */
    completed: boolean;
    /**
     * Movie media_item id, episode id, or track id
     */
    entity_id: number;
    /**
     * What's being played
     */
    entity_type: 'movie' | 'episode' | 'track';
    /**
     * How far into the item the player is
     */
    position_seconds: number;
    /**
     * Origin label: queue | radio | album | playlist | search | browse | similar
     */
    source?: string;
    /**
     * UTC Unix time when this playback began (track completion only)
     */
    started_at_unix?: number;
    /**
     * Total length (0 if unknown)
     */
    total_seconds: number;
};

export type PlaybackPrefBodyWritable = {
    audio_language: string;
    media_item_id: number;
    subtitle_language: string;
    subtitle_mode: string;
};

export type PlaylistDetailWritable = {
    has_cover: boolean;
    playlist: UserPlaylistWritable;
    syncs: Array<PlaylistSyncView> | null;
    tracks: Array<ListPlaylistTracksRow> | null;
};

export type PlaylistMutationWritable = {
    description: string;
    name: string;
    /**
     * Free-form organization tags; omit to keep existing
     */
    tags?: Array<string> | null;
};

export type PlaylistServiceCatalogWritable = {
    capabilities: Capabilities;
    collections: Array<PlaylistCollectionView> | null;
    playlists: Array<ExternalPlaylistView> | null;
    service: string;
};

export type PlaylistSyncToggleWritable = {
    enabled: boolean;
    mode?: 'two_way' | 'pull_only';
};

export type PlaylistsListBodyWritable = {
    items: Array<ListUserPlaylistsRow> | null;
};

export type PodcastCategoriesBodyWritable = {
    items: Array<Category> | null;
};

export type PodcastContinueBodyWritable = {
    items: Array<UserPodcastProgressWritable> | null;
};

export type PodcastDetailWritable = {
    artwork_url: string;
    author: string;
    categories: Array<string> | null;
    description: string;
    episodes: Array<PodcastEpisode> | null;
    feed_url: string;
    language: string;
    link: string;
    title: string;
};

export type PodcastProgressInputWritable = {
    artwork_url?: string;
    audio_url: string;
    completed: boolean;
    episode_guid: string;
    feed_url: string;
    progress_seconds: number;
    title: string;
    total_seconds: number;
};

export type PodcastSubsBodyWritable = {
    items: Array<UserPodcastSubscriptionWritable> | null;
};

export type PodcastsBodyWritable = {
    items: Array<Podcast> | null;
};

export type ProbeBodyWritable = {
    challenge: string;
};

export type QueueAdvanceRequestWritable = {
    /**
     * The item this renderer just finished/skipped — makes double-fires no-ops
     */
    from_item_id: number;
    reason: 'ended' | 'skip' | 'prev';
};

export type QueueClaimRequestWritable = {
    /**
     * local:<client_id> or cast:<device_id>
     */
    output: string;
};

export type QueueEnqueueRequestWritable = {
    at?: 'end' | 'next';
    track_ids: Array<number> | null;
};

export type QueueHeartbeatRequestWritable = {
    /**
     * This renderer's output id
     */
    output: string;
    playing: boolean;
    position_seconds: number;
};

export type QueueJumpRequestWritable = {
    item_id: number;
};

export type QueueMoveItemRequestWritable = {
    /**
     * Place after this item (0 = right after the current track)
     */
    after_item_id?: number;
};

export type QueueRepeatRequestWritable = {
    mode: 'off' | 'all' | 'one';
};

export type QueueReplaceRequestWritable = {
    /**
     * Claiming output id, e.g. local:<client_id>
     */
    output?: string;
    shuffle?: boolean;
    source: QueueSource;
    /**
     * Track to point at first (0 = head)
     */
    start_track_id?: number;
};

export type QueueShuffleRequestWritable = {
    on: boolean;
};

export type QueueViewWritable = {
    active_output?: string;
    current_index: number;
    current_item_id?: number;
    items: Array<QueueItemView> | null;
    playing: boolean;
    position_seconds: number;
    repeat_mode: string;
    shuffled: boolean;
    source?: QueueSource;
    total: number;
    version: number;
    window_start_index: number;
};

export type QuickSearchResultWritable = {
    buckets: {
        [key: string]: SearchBucketWritable;
    };
    query: string;
};

export type RadioCountriesBodyWritable = {
    items: Array<Country> | null;
};

export type RadioFavoritesBodyWritable = {
    items: Array<UserRadioFavoriteWritable> | null;
};

export type RadioRecentsBodyWritable = {
    items: Array<ListRadioRecentsRow> | null;
};

export type RadioRequestWritable = {
    /**
     * Tracks to skip (typically the current queue)
     */
    exclude_track_ids?: Array<number> | null;
    /**
     * 0..1 knob for how strongly candidates must share the seed's genre(s) to rank well. 0 (default) is a no-op; near 1 pushes zero-genre-overlap candidates to the bottom and, at >=0.9, drops them once enough overlapping candidates remain to fill the limit.
     */
    genre_affinity?: number;
    /**
     * Number of tracks to return
     */
    limit: number;
    seed: RadioSeed;
    /**
     * Optional. When populated, every seed is resolved to a track and their sonic embeddings are averaged into a centroid for KNN. Use to mix multiple artists/albums/tracks/vibes into one cohesive queue.
     */
    seeds?: Array<RadioSeed> | null;
};

export type RadioResponseWritable = {
    seed_track_id: number;
    /**
     * Similar canonical recordings that are not currently playable in this library
     */
    suggestions: Array<MusicCatalogSuggestion> | null;
    tracks: Array<SimilarTracksByTrackRichRow> | null;
};

export type RadioTagsBodyWritable = {
    items: Array<Tag> | null;
};

export type RailPageBodyWritable = {
    has_more: boolean;
    items: Array<RecRailItem> | null;
};

export type RatingBodyWritable = {
    rating: number;
};

export type ReadyBodyWritable = {
    components: Array<HealthComponent> | null;
    /**
     * 'ok' when all components healthy, 'degraded' otherwise
     */
    status: string;
};

export type RecentAlbumsBodyWritable = {
    items: Array<ListRecentlyAddedAlbumsRow> | null;
};

export type RecentArtistsBodyWritable = {
    items: Array<ListRecentlyPlayedArtistsRow> | null;
};

export type RecentPlaylistsBodyWritable = {
    items: Array<ListRecentUserPlaylistsRow> | null;
};

export type RecentlyPlayedBodyWritable = {
    items: Array<ListRecentlyPlayedTracksRow> | null;
};

export type RecommendationsMlSettingsWritable = {
    accelerator: string;
    enabled: boolean;
};

export type RecommendedResultWritable = {
    rails: Array<RecRail> | null;
};

export type RegisterInputBodyWritable = {
    /**
     * Email address
     */
    email: string;
    /**
     * Password
     */
    password: string;
    /**
     * Username
     */
    username: string;
};

export type RemoteConfigPayloadWritable = {
    acme_email?: string;
    /**
     * DNS provider for hostnames + certificates
     */
    dns_provider?: '' | 'desec' | 'duckdns' | 'cloudflare';
    /**
     * Provider API token (write-only; empty keeps existing)
     */
    dns_token?: string;
    /**
     * Zone managed at the provider (myname.dedyn.io, example.com)
     */
    domain?: string;
    enabled: boolean;
    /**
     * External+listener port; 0 = keep current / auto-generate
     */
    port?: number;
    /**
     * Optional label under the domain (heya → wan.heya.example.com)
     */
    subdomain?: string;
};

export type RemoteStatusBodyWritable = {
    available: boolean;
    config: RemoteConfigView;
    message?: string;
    status?: RemoteStatus;
};

export type ReorderListRequestWritable = {
    items: Array<ReorderItem> | null;
};

export type RequestWritable = {
    backend?: string;
    cfg?: number;
    device?: string;
    height?: number;
    memory_mode?: '' | 'auto' | 'low_vram';
    model_id?: string;
    negative_prompt?: string;
    prompt: string;
    seed?: number;
    steps?: number;
    width?: number;
};

export type RescueBodyWritable = {
    rescued: number;
    retries_reset: number;
};

export type ResolveMatchRequestWritable = {
    /**
     * Match candidate ID
     */
    candidate_id: number;
};

export type ScannerBulkApproveResultWritable = {
    approved: number;
};

export type ScannerBulkEligibleResultWritable = {
    eligible: number;
};

export type ScannerCandidateDetailViewWritable = {
    author?: string;
    backdrop_url?: string;
    candidate_id: number;
    description?: string;
    external_ids?: {
        [key: string]: string;
    };
    first_air_date?: string;
    genres?: Array<string> | null;
    heya_slug?: string;
    isbn?: string;
    language?: string;
    last_air_date?: string;
    networks?: Array<string> | null;
    number_of_episodes?: number;
    number_of_seasons?: number;
    page_count?: number;
    poster_url?: string;
    provider_id: string;
    provider_kind: string;
    provider_name: string;
    publish_date?: string;
    publisher?: string;
    runtime_minutes?: number;
    status?: string;
    subjects?: Array<string> | null;
    title: string;
    year?: string;
};

export type ScannerIdentityViewWritable = {
    bucket: string;
    candidate_count: number;
    confidence: number;
    id: number;
    identity_key: string;
    last_seen_scan_run_id?: number;
    library_id: number;
    main_finding_code?: string;
    main_finding_message?: string;
    main_finding_severity?: string;
    media_item_id?: number;
    media_type: string;
    metadata_provider_id?: string;
    open_finding_count: number;
    review_status: string;
    selected_provider_id?: string;
    selected_score?: number;
    selected_title?: string;
    selected_year?: string;
    source: string;
    title: string;
    updated_at?: string;
    year?: string;
};

export type ScannerOverviewWritable = {
    bucket_counts: ScannerBucketCounts;
    issue_counts: Array<ScannerIssueCount> | null;
    issue_total: number;
    latest_run?: ScannerRunView;
    pipeline_failures: Array<ScannerPipelineFailureView> | null;
};

export type SearchBucketWritable = {
    items: unknown;
    total: number;
};

export type SearchResponseWritable = {
    data: Array<SubtitleResult> | null;
    page: number;
    total_count: number;
    total_pages: number;
};

export type SemanticSearchResultWritable = {
    items: Array<ForYouItem> | null;
    ml_ready: boolean;
};

export type SessionCommandInputWritable = {
    action: 'stop' | 'message';
    message?: string;
};

export type SessionHeartbeatInputWritable = {
    audio_codec?: string;
    bitrate_kbps?: number;
    client_ip?: string;
    client_user_agent?: string;
    container?: string;
    entity_id?: number;
    entity_type?: string;
    file_id?: string;
    height?: number;
    media_item_id: number;
    paused: boolean;
    playback_action?: string;
    position_seconds: number;
    session_id: string;
    total_seconds: number;
    video_codec?: string;
    width?: number;
};

export type SessionSnapshotWritable = {
    album?: string;
    artist?: string;
    audio_track?: number;
    device_id: string;
    device_name: string;
    duration_sec?: number;
    entity_id?: number;
    entity_type?: string;
    file_id?: string;
    id: string;
    media_item_id?: number;
    media_kind?: string;
    position_sec: number;
    quality?: string;
    state: string;
    subtitle_track?: number;
    title?: string;
    track_id?: number;
    updated_at: string;
    user_id: number;
    volume: number;
};

export type SessionsBodyWritable = {
    items: Array<SessionSnapshotWritable> | null;
};

export type SetCastConfigRequestWritable = {
    /**
     * Regular users allowed to discover and control server-side cast receivers; admins are always allowed
     */
    allowed_user_ids: Array<number> | null;
    /**
     * Optional receiver-facing Heya origin for Chromecast/DLNA URL pulls; empty derives the routed LAN address
     */
    base_url: string;
    /**
     * Comma-separated receiver addresses resolved by unicast mDNS (same-subnet only)
     */
    devices: string;
    enabled: boolean;
};

export type SetPlaybackPreferenceRequestWritable = {
    /**
     * ISO 639-1/-2/-3 code or empty to clear
     */
    audio_language: string;
    /**
     * ISO 639-1/-2/-3 code or empty to clear
     */
    subtitle_language: string;
    /**
     * 'off' | 'forced' | 'full' | empty to clear
     */
    subtitle_mode: string;
};

export type SetPlaylistPinRequestWritable = {
    pinned: boolean;
    /**
     * Which pin set to toggle
     */
    scope: 'page' | 'sidebar';
};

export type SetPlaylistSidebarOrderRequestWritable = {
    /**
     * Playlist IDs in the desired top-to-bottom order (full list)
     */
    ids: Array<number> | null;
};

export type SetSystemSettingRequestWritable = {
    /**
     * JSON-encoded value
     */
    value: unknown;
};

export type SonicAnalysisSettingsWritable = {
    accelerator: string;
    enabled: boolean;
};

export type SonicSaveBodyWritable = {
    /**
     * Whether the new settings were live-applied or queued for next idle window
     */
    applied: boolean;
    status: string;
};

export type StationInputWritable = {
    bitrate?: number;
    clickcount?: number;
    codec?: string;
    country?: string;
    countrycode?: string;
    favicon?: string;
    homepage?: string;
    language?: string;
    name: string;
    stationuuid: string;
    tags?: string;
    url?: string;
    url_resolved?: string;
    votes?: number;
};

export type StationResponseWritable = {
    kind: string;
    label: string;
    tracks: Array<StationTrack> | null;
};

export type StationsBodyWritable = {
    items: Array<Station> | null;
};

export type StatusBodyWritable = {
    status: string;
};

export type StatusOutputBodyWritable = {
    /**
     * Action status
     */
    status: string;
};

export type StreamInfoResponseWritable = {
    audio: Array<AudioStream> | null;
    bit_rate: number;
    container: string;
    duration: number;
    library_id: number;
    playback: PlaybackDecision;
    qualities: Array<QualityOption> | null;
    size: number;
    subtitle: Array<SubStream> | null;
    video: Array<VideoStream> | null;
};

export type StudiosMediaIdsRequestWritable = {
    company_ids: Array<number> | null;
};

export type SubscribePodcastRequestWritable = {
    artwork_url: string;
    author: string;
    feed_url: string;
    title: string;
};

export type SubsonicConfigBodyWritable = {
    enabled: boolean;
};

export type SubsonicCredentialBodyWritable = {
    created_at: string;
    last_used_at?: string;
    rotated_at: string;
    secret: string;
};

export type SystemSettingBodyWritable = {
    key: string;
    value?: unknown;
};

export type TailscaleConfigPayloadWritable = {
    enabled: boolean;
    funnel: boolean;
    hostname: string;
    https: boolean;
};

export type TailscaleStatusBodyWritable = {
    config?: TailscaleConfigPayloadWritable;
    enabled: boolean;
    message?: string;
    status?: Status;
};

export type TaskItemsResultWritable = {
    complete: number;
    failed: number;
    items: Array<TaskItem> | null;
    pending: number;
    total: number;
};

export type TaskResponseWritable = {
    category: string;
    daily_end_time: string;
    daily_start_time: string;
    description: string;
    display_name: string;
    enabled: boolean;
    id: string;
    interval_hours: number;
    last_run_at: string | null;
    last_run_duration_sec: number;
    last_run_items_processed: number;
    last_run_items_total: number;
    last_run_result: string;
    max_runtime_minutes: number;
    next_run_at: string | null;
    runtime?: TaskRuntime;
    state: string;
    stats?: TaskStats;
};

export type TempoBucketsBodyWritable = {
    items: Array<TempoBucket> | null;
};

export type ToggleFavoriteRequestWritable = {
    entity_id: number;
    /**
     * Entity kind
     */
    entity_type: 'media_item' | 'episode' | 'season' | 'track' | 'artist' | 'album';
};

export type ToggleTailscaleFunnelRequestWritable = {
    enabled: boolean;
};

export type TopTracksBodyWritable = {
    items: Array<ArtistTopTrackRow> | null;
};

export type TrackIdsBodyWritable = {
    /**
     * List of track IDs to look up
     */
    track_ids: Array<number> | null;
};

export type TrackResultsBodyWritable = {
    items: Array<SimilarTracksByTrackRichRow> | null;
};

export type TrackTextSearchBodyWritable = {
    items: Array<SimilarTracksByTextRichRow> | null;
};

export type TranscodeProgressResponseWritable = {
    active: boolean;
    bitrate_kbps: number;
    drop_frames: number;
    dup_frames: number;
    elapsed_seconds: number;
    fps: number;
    frame: number;
    head_current_segment: number;
    head_start_segment: number;
    head_stop_reason?: string;
    last_requested_segment: number;
    last_update_ago_ms: number;
    lead_cap_seconds: number;
    out_time_seconds: number;
    ready_segments: number;
    running: boolean;
    session_key?: string;
    speed: number;
    started_at_unix_ms?: number;
    state: string;
    total_segments: number;
    total_size_bytes: number;
    updated_at_unix_ms?: number;
};

export type TranscodeStatusBodyWritable = {
    active_jobs: number;
    available: boolean;
    cache_dir: string;
    cache_items: number;
    cache_max_gb: number;
    cache_size_mb: number;
    config_mode: string;
    encoder_h264: string;
    encoder_hevc: string;
    hw_accel: string;
    hw_accel_label: string;
};

export type TvEpisodeWritable = {
    absolute_number: number;
    air_date: Date;
    episode_number: number;
    episode_type: number;
    external_ids: string;
    id: number;
    is_special: boolean;
    overview: string;
    rating: Numeric;
    runtime_minutes: number;
    season_id: number;
    source: string;
    still_path: string;
    title: string;
};

export type TvSeasonWritable = {
    air_date: Date;
    aired_episodes: number;
    end_date: Date;
    external_ids: string;
    id: number;
    overview: string;
    poster_path: string;
    season_number: number;
    series_id: number;
    status: string;
    title: string;
};

export type UpNextResultWritable = {
    episode_id?: number;
    episode_number?: number;
    episode_title?: string;
    file_id?: number;
    file_public_id?: string;
    has_next: boolean;
    media_item_id?: number;
    media_item_public_id?: string;
    runtime?: number;
    season_id?: number;
    season_number?: number;
};

export type UpdateLibraryRequestWritable = {
    name: string;
    /**
     * Absolute filesystem directory paths visible to the Heya host or container; mount network shares before configuring them
     */
    paths: Array<string> | null;
};

export type UpdateTaskRequestWritable = {
    daily_end_time: string;
    /**
     * HH:MM 24h or empty
     */
    daily_start_time: string;
    enabled: boolean;
    interval_hours: number;
    max_runtime_minutes: number;
};

export type UpdateTranscodeSettingsRequestWritable = {
    cache_max_gb: number;
    hw_accel: 'auto' | 'none' | 'vaapi' | 'qsv' | 'nvenc' | 'videotoolbox';
};

export type UpdateUserListRequestWritable = {
    description: string;
    filter_json: unknown;
    icon: string;
    name: string;
};

export type UpdateAlbumReqWritable = {
    album_type?: string;
    barcode?: string;
    country?: string;
    genres?: Array<string> | null;
    label?: string;
    release_date?: string;
    title?: string;
    year?: string;
};

export type UpdateEpisodeReqWritable = {
    air_date?: string;
    overview?: string;
    runtime_minutes?: number;
    title?: string;
};

export type UpdateMediaMetadataReqWritable = {
    author_name?: string;
    biography?: string;
    description?: string;
    disambiguation?: string;
    external_ids?: {
        [key: string]: string;
    };
    first_air_date?: string;
    format?: string;
    genres?: Array<string> | null;
    isbn?: string;
    language?: string;
    last_air_date?: string;
    networks?: Array<string> | null;
    original_language?: string;
    original_name?: string;
    original_title?: string;
    page_count?: number;
    publish_date?: string;
    publisher?: string;
    release_date?: string;
    runtime_minutes?: number;
    series_name?: string;
    series_number?: number;
    sort_name?: string;
    sort_title?: string;
    status?: string;
    subjects?: Array<string> | null;
    tagline?: string;
    title?: string;
    year?: string;
};

export type UpdateSeasonReqWritable = {
    air_date?: string;
    overview?: string;
    title?: string;
};

export type UploadAssetResultBodyWritable = {
    asset?: MediaAsset;
    path?: string;
    status: string;
};

export type UserInfoWritable = {
    allowed_downloads: number;
    allowed_translations: number;
    ext_installed: boolean;
    level: string;
    remaining_downloads: number;
    user_id: number;
    vip: boolean;
};

export type UserListDetailBodyWritable = {
    items: Array<MediaItemCard> | null;
    list: UserList;
};

export type UserListItemWritable = {
    added_at: Timestamptz;
    id: number;
    list_id: number;
    media_item_id: number;
    sort_order: number;
};

export type UserListViewWritable = {
    contains?: boolean;
    created_at: Timestamptz;
    description: string;
    filter_json: unknown;
    icon: string;
    id: number;
    item_count: number;
    list_type: string;
    media_type: string;
    name: string;
    updated_at: Timestamptz;
    user_id: number;
};

export type UserPlaylistWritable = {
    cover_path: string;
    created_at: Timestamptz;
    description: string;
    id: number;
    name: string;
    pinned: boolean;
    sidebar_pinned: boolean;
    sidebar_position: number;
    slug: string;
    tags: Array<string> | null;
    updated_at: Timestamptz;
    user_id: number;
};

export type UserPodcastProgressWritable = {
    artwork_url: string;
    audio_url: string;
    completed: boolean;
    episode_guid: string;
    feed_url: string;
    id: number;
    progress_seconds: number;
    title: string;
    total_seconds: number;
    updated_at: Timestamptz;
    user_id: number;
};

export type UserPodcastSubscriptionWritable = {
    artwork_url: string;
    author: string;
    created_at: Timestamptz;
    feed_url: string;
    id: number;
    last_episode_at: Timestamptz;
    title: string;
    user_id: number;
};

export type UserRadioFavoriteWritable = {
    bitrate: number;
    codec: string;
    country: string;
    countrycode: string;
    created_at: Timestamptz;
    favicon: string;
    homepage: string;
    id: number;
    language: string;
    name: string;
    stationuuid: string;
    tags: string;
    url: string;
    user_id: number;
};

export type UserSettingsWritable = {
    appearance: AppearanceSettings;
    home: HomeSettings;
    playback: PlaybackSettings;
    ui: UiSettings;
};

export type UserViewWritable = {
    email: string;
    id: number;
    is_admin: boolean;
    username: string;
};

export type WatchedBodyWritable = {
    watched: boolean;
};

export type WatcherStatusBodyWritable = {
    count: number;
    /**
     * Time of the most recent worker heartbeat
     */
    updated_at?: string;
    watchers: Array<WatcherEntry> | null;
    /**
     * Whether the dedicated worker heartbeat is current
     */
    worker_online: boolean;
};

export type WaveformBodyWritable = {
    waveform: unknown;
};

export type ActivityFeedData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/activity';
};

export type ActivityFeedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ActivityFeedError = ActivityFeedErrors[keyof ActivityFeedErrors];

export type ActivityFeedResponses = {
    /**
     * OK
     */
    200: Array<ActivityItem> | null;
};

export type ActivityFeedResponse = ActivityFeedResponses[keyof ActivityFeedResponses];

export type AdminDbData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/db';
};

export type AdminDbErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminDbError = AdminDbErrors[keyof AdminDbErrors];

export type AdminDbResponses = {
    /**
     * OK
     */
    200: AdminDbBody;
};

export type AdminDbResponse = AdminDbResponses[keyof AdminDbResponses];

export type AdminDiagnosticsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/diagnostics';
};

export type AdminDiagnosticsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminDiagnosticsError = AdminDiagnosticsErrors[keyof AdminDiagnosticsErrors];

export type AdminDiagnosticsResponses = {
    /**
     * OK
     */
    200: AdminDiagnosticsBody;
};

export type AdminDiagnosticsResponse = AdminDiagnosticsResponses[keyof AdminDiagnosticsResponses];

export type AdminDoctorData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/doctor';
};

export type AdminDoctorErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminDoctorError = AdminDoctorErrors[keyof AdminDoctorErrors];

export type AdminDoctorResponses = {
    /**
     * OK
     */
    200: DoctorReport;
};

export type AdminDoctorResponse = AdminDoctorResponses[keyof AdminDoctorResponses];

export type AdminListenersData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/listeners';
};

export type AdminListenersErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminListenersError = AdminListenersErrors[keyof AdminListenersErrors];

export type AdminListenersResponses = {
    /**
     * OK
     */
    200: AdminListenersBody;
};

export type AdminListenersResponse = AdminListenersResponses[keyof AdminListenersResponses];

export type AdminGetLogLevelData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/log-level';
};

export type AdminGetLogLevelErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminGetLogLevelError = AdminGetLogLevelErrors[keyof AdminGetLogLevelErrors];

export type AdminGetLogLevelResponses = {
    /**
     * OK
     */
    200: AdminLogLevelBody;
};

export type AdminGetLogLevelResponse = AdminGetLogLevelResponses[keyof AdminGetLogLevelResponses];

export type AdminSetLogLevelData = {
    body: AdminSetLogLevelRequestWritable;
    path?: never;
    query?: never;
    url: '/api/admin/log-level';
};

export type AdminSetLogLevelErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminSetLogLevelError = AdminSetLogLevelErrors[keyof AdminSetLogLevelErrors];

export type AdminSetLogLevelResponses = {
    /**
     * OK
     */
    200: AdminLogLevelBody;
};

export type AdminSetLogLevelResponse = AdminSetLogLevelResponses[keyof AdminSetLogLevelResponses];

export type AdminNetworkStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/network/status';
};

export type AdminNetworkStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminNetworkStatusError = AdminNetworkStatusErrors[keyof AdminNetworkStatusErrors];

export type AdminNetworkStatusResponses = {
    /**
     * OK
     */
    200: AdminNetworkStatusBody;
};

export type AdminNetworkStatusResponse = AdminNetworkStatusResponses[keyof AdminNetworkStatusResponses];

export type RecommendationsMlBackfillData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/recommendations-ml/backfill';
};

export type RecommendationsMlBackfillErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecommendationsMlBackfillError = RecommendationsMlBackfillErrors[keyof RecommendationsMlBackfillErrors];

export type RecommendationsMlBackfillResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RecommendationsMlBackfillResponse = RecommendationsMlBackfillResponses[keyof RecommendationsMlBackfillResponses];

export type RecommendationsMlFetchData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/recommendations-ml/fetch';
};

export type RecommendationsMlFetchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecommendationsMlFetchError = RecommendationsMlFetchErrors[keyof RecommendationsMlFetchErrors];

export type RecommendationsMlFetchResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RecommendationsMlFetchResponse = RecommendationsMlFetchResponses[keyof RecommendationsMlFetchResponses];

export type GetRecommendationsMlSettingsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/recommendations-ml/settings';
};

export type GetRecommendationsMlSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetRecommendationsMlSettingsError = GetRecommendationsMlSettingsErrors[keyof GetRecommendationsMlSettingsErrors];

export type GetRecommendationsMlSettingsResponses = {
    /**
     * OK
     */
    200: RecommendationsMlSettings;
};

export type GetRecommendationsMlSettingsResponse = GetRecommendationsMlSettingsResponses[keyof GetRecommendationsMlSettingsResponses];

export type SetRecommendationsMlSettingsData = {
    body: RecommendationsMlSettingsWritable;
    path?: never;
    query?: never;
    url: '/api/admin/recommendations-ml/settings';
};

export type SetRecommendationsMlSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetRecommendationsMlSettingsError = SetRecommendationsMlSettingsErrors[keyof SetRecommendationsMlSettingsErrors];

export type SetRecommendationsMlSettingsResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type SetRecommendationsMlSettingsResponse = SetRecommendationsMlSettingsResponses[keyof SetRecommendationsMlSettingsResponses];

export type RecommendationsMlStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/recommendations-ml/status';
};

export type RecommendationsMlStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecommendationsMlStatusError = RecommendationsMlStatusErrors[keyof RecommendationsMlStatusErrors];

export type RecommendationsMlStatusResponses = {
    /**
     * OK
     */
    200: {
        [key: string]: unknown;
    };
};

export type RecommendationsMlStatusResponse = RecommendationsMlStatusResponses[keyof RecommendationsMlStatusResponses];

export type AdminListSessionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/sessions';
};

export type AdminListSessionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminListSessionsError = AdminListSessionsErrors[keyof AdminListSessionsErrors];

export type AdminListSessionsResponses = {
    /**
     * OK
     */
    200: Array<AdminSessionView> | null;
};

export type AdminListSessionsResponse = AdminListSessionsResponses[keyof AdminListSessionsResponses];

export type AdminRevokeSessionData = {
    body?: never;
    path: {
        /**
         * Session id
         */
        id: number;
    };
    query?: never;
    url: '/api/admin/sessions/{id}';
};

export type AdminRevokeSessionErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminRevokeSessionError = AdminRevokeSessionErrors[keyof AdminRevokeSessionErrors];

export type AdminRevokeSessionResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type AdminRevokeSessionResponse = AdminRevokeSessionResponses[keyof AdminRevokeSessionResponses];

export type TriggerSonicFetchData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/sonicanalysis/fetch';
};

export type TriggerSonicFetchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TriggerSonicFetchError = TriggerSonicFetchErrors[keyof TriggerSonicFetchErrors];

export type TriggerSonicFetchResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type TriggerSonicFetchResponse = TriggerSonicFetchResponses[keyof TriggerSonicFetchResponses];

export type GetSonicAnalysisSettingsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/sonicanalysis/settings';
};

export type GetSonicAnalysisSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetSonicAnalysisSettingsError = GetSonicAnalysisSettingsErrors[keyof GetSonicAnalysisSettingsErrors];

export type GetSonicAnalysisSettingsResponses = {
    /**
     * OK
     */
    200: SonicAnalysisSettings;
};

export type GetSonicAnalysisSettingsResponse = GetSonicAnalysisSettingsResponses[keyof GetSonicAnalysisSettingsResponses];

export type SetSonicAnalysisSettingsData = {
    body: SonicAnalysisSettingsWritable;
    path?: never;
    query?: never;
    url: '/api/admin/sonicanalysis/settings';
};

export type SetSonicAnalysisSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetSonicAnalysisSettingsError = SetSonicAnalysisSettingsErrors[keyof SetSonicAnalysisSettingsErrors];

export type SetSonicAnalysisSettingsResponses = {
    /**
     * OK
     */
    200: SonicSaveBody;
};

export type SetSonicAnalysisSettingsResponse = SetSonicAnalysisSettingsResponses[keyof SetSonicAnalysisSettingsResponses];

export type SonicAnalysisStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/sonicanalysis/status';
};

export type SonicAnalysisStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SonicAnalysisStatusError = SonicAnalysisStatusErrors[keyof SonicAnalysisStatusErrors];

export type SonicAnalysisStatusResponses = {
    /**
     * OK
     */
    200: {
        [key: string]: unknown;
    };
};

export type SonicAnalysisStatusResponse = SonicAnalysisStatusResponses[keyof SonicAnalysisStatusResponses];

export type AdminStorageData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/storage';
};

export type AdminStorageErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminStorageError = AdminStorageErrors[keyof AdminStorageErrors];

export type AdminStorageResponses = {
    /**
     * OK
     */
    200: AdminStorageBody;
};

export type AdminStorageResponse = AdminStorageResponses[keyof AdminStorageResponses];

export type AdminStorageScanData = {
    body: AdminStorageScanRequestWritable;
    path?: never;
    query?: never;
    url: '/api/admin/storage/scan';
};

export type AdminStorageScanErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminStorageScanError = AdminStorageScanErrors[keyof AdminStorageScanErrors];

export type AdminStorageScanResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type AdminStorageScanResponse = AdminStorageScanResponses[keyof AdminStorageScanResponses];

export type AdminSystemData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/system';
};

export type AdminSystemErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminSystemError = AdminSystemErrors[keyof AdminSystemErrors];

export type AdminSystemResponses = {
    /**
     * OK
     */
    200: AdminSystemBody;
};

export type AdminSystemResponse = AdminSystemResponses[keyof AdminSystemResponses];

export type AdminListUsersData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/users';
};

export type AdminListUsersErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminListUsersError = AdminListUsersErrors[keyof AdminListUsersErrors];

export type AdminListUsersResponses = {
    /**
     * OK
     */
    200: Array<AdminUserView> | null;
};

export type AdminListUsersResponse = AdminListUsersResponses[keyof AdminListUsersResponses];

export type AdminCreateUserData = {
    body: AdminCreateUserRequestWritable;
    path?: never;
    query?: never;
    url: '/api/admin/users';
};

export type AdminCreateUserErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminCreateUserError = AdminCreateUserErrors[keyof AdminCreateUserErrors];

export type AdminCreateUserResponses = {
    /**
     * OK
     */
    200: AdminUserView;
};

export type AdminCreateUserResponse = AdminCreateUserResponses[keyof AdminCreateUserResponses];

export type AdminDeleteUserData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/admin/users/{id}';
};

export type AdminDeleteUserErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminDeleteUserError = AdminDeleteUserErrors[keyof AdminDeleteUserErrors];

export type AdminDeleteUserResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type AdminDeleteUserResponse = AdminDeleteUserResponses[keyof AdminDeleteUserResponses];

export type AdminResetUserPasswordData = {
    body: AdminResetUserPasswordRequestWritable;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/admin/users/{id}/password';
};

export type AdminResetUserPasswordErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminResetUserPasswordError = AdminResetUserPasswordErrors[keyof AdminResetUserPasswordErrors];

export type AdminResetUserPasswordResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type AdminResetUserPasswordResponse = AdminResetUserPasswordResponses[keyof AdminResetUserPasswordResponses];

export type AdminSetUserRoleData = {
    body: AdminSetUserRoleRequestWritable;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/admin/users/{id}/role';
};

export type AdminSetUserRoleErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminSetUserRoleError = AdminSetUserRoleErrors[keyof AdminSetUserRoleErrors];

export type AdminSetUserRoleResponses = {
    /**
     * OK
     */
    200: AdminUserView;
};

export type AdminSetUserRoleResponse = AdminSetUserRoleResponses[keyof AdminSetUserRoleResponses];

export type AdminWorkersData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/admin/workers';
};

export type AdminWorkersErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AdminWorkersError = AdminWorkersErrors[keyof AdminWorkersErrors];

export type AdminWorkersResponses = {
    /**
     * OK
     */
    200: AdminWorkersBody;
};

export type AdminWorkersResponse = AdminWorkersResponses[keyof AdminWorkersResponses];

export type GetAiCatalogData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/catalog';
};

export type GetAiCatalogErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAiCatalogError = GetAiCatalogErrors[keyof GetAiCatalogErrors];

export type GetAiCatalogResponses = {
    /**
     * OK
     */
    200: AiCatalogBody;
};

export type GetAiCatalogResponse = GetAiCatalogResponses[keyof GetAiCatalogResponses];

export type PostAiChatData = {
    body: AiChatRequestWritable;
    path?: never;
    query?: never;
    url: '/api/ai/chat';
};

export type PostAiChatErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PostAiChatError = PostAiChatErrors[keyof PostAiChatErrors];

export type PostAiChatResponses = {
    /**
     * OK
     */
    200: AiChatResponse;
};

export type PostAiChatResponse = PostAiChatResponses[keyof PostAiChatResponses];

export type GetAiImageCatalogData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/images/catalog';
};

export type GetAiImageCatalogErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAiImageCatalogError = GetAiImageCatalogErrors[keyof GetAiImageCatalogErrors];

export type GetAiImageCatalogResponses = {
    /**
     * OK
     */
    200: ImageCatalogBody;
};

export type GetAiImageCatalogResponse = GetAiImageCatalogResponses[keyof GetAiImageCatalogResponses];

export type PostAiImageFetchData = {
    body: ImageFetchBodyWritable;
    path?: never;
    query?: never;
    url: '/api/ai/images/fetch';
};

export type PostAiImageFetchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PostAiImageFetchError = PostAiImageFetchErrors[keyof PostAiImageFetchErrors];

export type PostAiImageFetchResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type PostAiImageFetchResponse = PostAiImageFetchResponses[keyof PostAiImageFetchResponses];

export type PostAiImageGenerateData = {
    body: RequestWritable;
    path?: never;
    query?: never;
    url: '/api/ai/images/generate';
};

export type PostAiImageGenerateErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PostAiImageGenerateError = PostAiImageGenerateErrors[keyof PostAiImageGenerateErrors];

export type PostAiImageGenerateResponses = {
    /**
     * OK
     */
    200: ImageGenerateBody;
};

export type PostAiImageGenerateResponse = PostAiImageGenerateResponses[keyof PostAiImageGenerateResponses];

export type GetAiImageStatusData = {
    body?: never;
    path?: never;
    query?: {
        model?: string;
        backend?: string;
    };
    url: '/api/ai/images/status';
};

export type GetAiImageStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAiImageStatusError = GetAiImageStatusErrors[keyof GetAiImageStatusErrors];

export type GetAiImageStatusResponses = {
    /**
     * OK
     */
    200: ImageStatus;
};

export type GetAiImageStatusResponse = GetAiImageStatusResponses[keyof GetAiImageStatusResponses];

export type AiGeneratedImageData = {
    body?: never;
    path: {
        filename: string;
    };
    query?: never;
    url: '/api/ai/images/{filename}';
};

export type AiGeneratedImageErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AiGeneratedImageError = AiGeneratedImageErrors[keyof AiGeneratedImageErrors];

export type AiGeneratedImageResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type AiGeneratedImageResponse = AiGeneratedImageResponses[keyof AiGeneratedImageResponses];

export type PostAiLocalDownloadData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/local/download';
};

export type PostAiLocalDownloadErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PostAiLocalDownloadError = PostAiLocalDownloadErrors[keyof PostAiLocalDownloadErrors];

export type PostAiLocalDownloadResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type PostAiLocalDownloadResponse = PostAiLocalDownloadResponses[keyof PostAiLocalDownloadResponses];

export type PostAiLocalStopData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/local/stop';
};

export type PostAiLocalStopErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PostAiLocalStopError = PostAiLocalStopErrors[keyof PostAiLocalStopErrors];

export type PostAiLocalStopResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type PostAiLocalStopResponse = PostAiLocalStopResponses[keyof PostAiLocalStopResponses];

export type GetAiModelsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/models';
};

export type GetAiModelsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAiModelsError = GetAiModelsErrors[keyof GetAiModelsErrors];

export type GetAiModelsResponses = {
    /**
     * OK
     */
    200: AiModelsBody;
};

export type GetAiModelsResponse = GetAiModelsResponses[keyof GetAiModelsResponses];

export type PostAiMusicMixData = {
    body: AiMusicMixRequestWritable;
    path?: never;
    query?: never;
    url: '/api/ai/music-mix';
};

export type PostAiMusicMixErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PostAiMusicMixError = PostAiMusicMixErrors[keyof PostAiMusicMixErrors];

export type PostAiMusicMixResponses = {
    /**
     * OK
     */
    200: AiMusicMixResult;
};

export type PostAiMusicMixResponse = PostAiMusicMixResponses[keyof PostAiMusicMixResponses];

export type GetAiReadyData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/ready';
};

export type GetAiReadyErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAiReadyError = GetAiReadyErrors[keyof GetAiReadyErrors];

export type GetAiReadyResponses = {
    /**
     * OK
     */
    200: AiReadyBody;
};

export type GetAiReadyResponse = GetAiReadyResponses[keyof GetAiReadyResponses];

export type PostAiRecommendData = {
    body: AiRecommendRequestWritable;
    path?: never;
    query?: never;
    url: '/api/ai/recommend';
};

export type PostAiRecommendErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PostAiRecommendError = PostAiRecommendErrors[keyof PostAiRecommendErrors];

export type PostAiRecommendResponses = {
    /**
     * OK
     */
    200: AiRecommendResult;
};

export type PostAiRecommendResponse = PostAiRecommendResponses[keyof PostAiRecommendResponses];

export type GetAiSettingsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/settings';
};

export type GetAiSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAiSettingsError = GetAiSettingsErrors[keyof GetAiSettingsErrors];

export type GetAiSettingsResponses = {
    /**
     * OK
     */
    200: AiSettingsView;
};

export type GetAiSettingsResponse = GetAiSettingsResponses[keyof GetAiSettingsResponses];

export type SetAiSettingsData = {
    body: AiSettingsWritable;
    path?: never;
    query?: never;
    url: '/api/ai/settings';
};

export type SetAiSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetAiSettingsError = SetAiSettingsErrors[keyof SetAiSettingsErrors];

export type SetAiSettingsResponses = {
    /**
     * OK
     */
    200: AiSettingsView;
};

export type SetAiSettingsResponse = SetAiSettingsResponses[keyof SetAiSettingsResponses];

export type GetAiStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/ai/status';
};

export type GetAiStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAiStatusError = GetAiStatusErrors[keyof GetAiStatusErrors];

export type GetAiStatusResponses = {
    /**
     * OK
     */
    200: AiStatusReport;
};

export type GetAiStatusResponse = GetAiStatusResponses[keyof GetAiStatusResponses];

export type LoginData = {
    body: LoginInputBodyWritable;
    headers?: {
        /**
         * Captured into the session so the user can recognise this device on the My Sessions page
         */
        'User-Agent'?: string;
    };
    path?: never;
    query?: never;
    url: '/api/auth/login';
};

export type LoginErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LoginError = LoginErrors[keyof LoginErrors];

export type LoginResponses = {
    /**
     * OK
     */
    200: AuthBody;
};

export type LoginResponse = LoginResponses[keyof LoginResponses];

export type LogoutData = {
    body?: never;
    headers?: {
        /**
         * Bearer <token>
         */
        Authorization?: string;
        Cookie?: string;
    };
    path?: never;
    query?: never;
    url: '/api/auth/logout';
};

export type LogoutErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LogoutError = LogoutErrors[keyof LogoutErrors];

export type LogoutResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type LogoutResponse = LogoutResponses[keyof LogoutResponses];

export type MeData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/auth/me';
};

export type MeErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MeError = MeErrors[keyof MeErrors];

export type MeResponses = {
    /**
     * OK
     */
    200: UserView;
};

export type MeResponse = MeResponses[keyof MeResponses];

export type RegisterData = {
    body: RegisterInputBodyWritable;
    headers?: {
        /**
         * Captured into the session so the user can recognise this device on the My Sessions page
         */
        'User-Agent'?: string;
    };
    path?: never;
    query?: never;
    url: '/api/auth/register';
};

export type RegisterErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RegisterError = RegisterErrors[keyof RegisterErrors];

export type RegisterResponses = {
    /**
     * OK
     */
    200: AuthBody;
};

export type RegisterResponse = RegisterResponses[keyof RegisterResponses];

export type CastConfigData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/cast/config';
};

export type CastConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastConfigError = CastConfigErrors[keyof CastConfigErrors];

export type CastConfigResponses = {
    /**
     * OK
     */
    200: CastConfigView;
};

export type CastConfigResponse = CastConfigResponses[keyof CastConfigResponses];

export type SetCastConfigData = {
    body: SetCastConfigRequestWritable;
    path?: never;
    query?: never;
    url: '/api/cast/config';
};

export type SetCastConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetCastConfigError = SetCastConfigErrors[keyof SetCastConfigErrors];

export type SetCastConfigResponses = {
    /**
     * OK
     */
    200: CastConfigView;
};

export type SetCastConfigResponse = SetCastConfigResponses[keyof SetCastConfigResponses];

export type CastDevicesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/cast/devices';
};

export type CastDevicesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastDevicesError = CastDevicesErrors[keyof CastDevicesErrors];

export type CastDevicesResponses = {
    /**
     * OK
     */
    200: DevicesBody;
};

export type CastDevicesResponse = CastDevicesResponses[keyof CastDevicesResponses];

export type CastStreamTrackData = {
    body?: never;
    path: {
        id: number;
    };
    query?: {
        cast_token?: string;
    };
    url: '/api/cast/media/music/{id}';
};

export type CastStreamTrackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStreamTrackError = CastStreamTrackErrors[keyof CastStreamTrackErrors];

export type CastStreamTrackResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type CastStreamTrackResponse = CastStreamTrackResponses[keyof CastStreamTrackResponses];

export type CastStreamVideoData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: {
        cast_token?: string;
    };
    url: '/api/cast/media/video/{file_id}';
};

export type CastStreamVideoErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStreamVideoError = CastStreamVideoErrors[keyof CastStreamVideoErrors];

export type CastStreamVideoResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type CastStreamVideoResponse = CastStreamVideoResponses[keyof CastStreamVideoResponses];

export type CastStreamVideoHlsIndexData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: {
        cast_token?: string;
    };
    url: '/api/cast/media/video/{file_id}/hls/index.m3u8';
};

export type CastStreamVideoHlsIndexErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStreamVideoHlsIndexError = CastStreamVideoHlsIndexErrors[keyof CastStreamVideoHlsIndexErrors];

export type CastStreamVideoHlsIndexResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type CastStreamVideoHlsIndexResponse = CastStreamVideoHlsIndexResponses[keyof CastStreamVideoHlsIndexResponses];

export type CastStreamVideoHlsMasterData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: {
        cast_token?: string;
    };
    url: '/api/cast/media/video/{file_id}/hls/master.m3u8';
};

export type CastStreamVideoHlsMasterErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStreamVideoHlsMasterError = CastStreamVideoHlsMasterErrors[keyof CastStreamVideoHlsMasterErrors];

export type CastStreamVideoHlsMasterResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type CastStreamVideoHlsMasterResponse = CastStreamVideoHlsMasterResponses[keyof CastStreamVideoHlsMasterResponses];

export type CastStreamVideoHlsSegmentData = {
    body?: never;
    path: {
        file_id: string;
        segment: string;
    };
    query?: {
        cast_token?: string;
    };
    url: '/api/cast/media/video/{file_id}/hls/{segment}';
};

export type CastStreamVideoHlsSegmentErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStreamVideoHlsSegmentError = CastStreamVideoHlsSegmentErrors[keyof CastStreamVideoHlsSegmentErrors];

export type CastStreamVideoHlsSegmentResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type CastStreamVideoHlsSegmentResponse = CastStreamVideoHlsSegmentResponses[keyof CastStreamVideoHlsSegmentResponses];

export type CastStreamVideoSubtitleData = {
    body?: never;
    path: {
        file_id: string;
        index: number;
    };
    query?: {
        cast_token?: string;
    };
    url: '/api/cast/media/video/{file_id}/subtitles/{index}';
};

export type CastStreamVideoSubtitleErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStreamVideoSubtitleError = CastStreamVideoSubtitleErrors[keyof CastStreamVideoSubtitleErrors];

export type CastStreamVideoSubtitleResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type CastStreamVideoSubtitleResponse = CastStreamVideoSubtitleResponses[keyof CastStreamVideoSubtitleResponses];

export type CastSessionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/cast/sessions';
};

export type CastSessionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastSessionsError = CastSessionsErrors[keyof CastSessionsErrors];

export type CastSessionsResponses = {
    /**
     * OK
     */
    200: SessionsBody;
};

export type CastSessionsResponse = CastSessionsResponses[keyof CastSessionsResponses];

export type CastPlayData = {
    body: CastPlayRequestWritable;
    path?: never;
    query?: never;
    url: '/api/cast/sessions';
};

export type CastPlayErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastPlayError = CastPlayErrors[keyof CastPlayErrors];

export type CastPlayResponses = {
    /**
     * OK
     */
    200: SessionSnapshot;
};

export type CastPlayResponse = CastPlayResponses[keyof CastPlayResponses];

export type CastSessionData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/api/cast/sessions/{id}';
};

export type CastSessionErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastSessionError = CastSessionErrors[keyof CastSessionErrors];

export type CastSessionResponses = {
    /**
     * OK
     */
    200: SessionSnapshot;
};

export type CastSessionResponse = CastSessionResponses[keyof CastSessionResponses];

export type CastPauseData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/api/cast/sessions/{id}/pause';
};

export type CastPauseErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastPauseError = CastPauseErrors[keyof CastPauseErrors];

export type CastPauseResponses = {
    /**
     * OK
     */
    200: SessionSnapshot;
};

export type CastPauseResponse = CastPauseResponses[keyof CastPauseResponses];

export type CastResumeData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/api/cast/sessions/{id}/resume';
};

export type CastResumeErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastResumeError = CastResumeErrors[keyof CastResumeErrors];

export type CastResumeResponses = {
    /**
     * OK
     */
    200: SessionSnapshot;
};

export type CastResumeResponse = CastResumeResponses[keyof CastResumeResponses];

export type CastSeekData = {
    body: CastSeekRequestWritable;
    path: {
        id: string;
    };
    query?: never;
    url: '/api/cast/sessions/{id}/seek';
};

export type CastSeekErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastSeekError = CastSeekErrors[keyof CastSeekErrors];

export type CastSeekResponses = {
    /**
     * OK
     */
    200: SessionSnapshot;
};

export type CastSeekResponse = CastSeekResponses[keyof CastSeekResponses];

export type CastStopData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/api/cast/sessions/{id}/stop';
};

export type CastStopErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStopError = CastStopErrors[keyof CastStopErrors];

export type CastStopResponses = {
    /**
     * OK
     */
    200: SessionSnapshot;
};

export type CastStopResponse = CastStopResponses[keyof CastStopResponses];

export type CastVolumeData = {
    body: CastVolumeRequestWritable;
    path: {
        id: string;
    };
    query?: never;
    url: '/api/cast/sessions/{id}/volume';
};

export type CastVolumeErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastVolumeError = CastVolumeErrors[keyof CastVolumeErrors];

export type CastVolumeResponses = {
    /**
     * OK
     */
    200: SessionSnapshot;
};

export type CastVolumeResponse = CastVolumeResponses[keyof CastVolumeResponses];

export type CastStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/cast/status';
};

export type CastStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CastStatusError = CastStatusErrors[keyof CastStatusErrors];

export type CastStatusResponses = {
    /**
     * OK
     */
    200: CastNetworkStatus;
};

export type CastStatusResponse = CastStatusResponses[keyof CastStatusResponses];

export type ListCollectionsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/collections';
};

export type ListCollectionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListCollectionsError = ListCollectionsErrors[keyof ListCollectionsErrors];

export type ListCollectionsResponses = {
    /**
     * OK
     */
    200: CollectionListResult;
};

export type ListCollectionsResponse = ListCollectionsResponses[keyof ListCollectionsResponses];

export type BrowseCollectionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/collections/browse';
};

export type BrowseCollectionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BrowseCollectionsError = BrowseCollectionsErrors[keyof BrowseCollectionsErrors];

export type BrowseCollectionsResponses = {
    /**
     * OK
     */
    200: Array<ListCollectionsWithLocalMediaRow> | null;
};

export type BrowseCollectionsResponse = BrowseCollectionsResponses[keyof BrowseCollectionsResponses];

export type GetCollectionData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/collections/{id}';
};

export type GetCollectionErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetCollectionError = GetCollectionErrors[keyof GetCollectionErrors];

export type GetCollectionResponses = {
    /**
     * OK
     */
    200: CollectionResult;
};

export type GetCollectionResponse = GetCollectionResponses[keyof GetCollectionResponses];

export type GetConfigSourcesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/config/sources';
};

export type GetConfigSourcesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetConfigSourcesError = GetConfigSourcesErrors[keyof GetConfigSourcesErrors];

export type GetConfigSourcesResponses = {
    /**
     * OK
     */
    200: {
        [key: string]: SourceEntry;
    };
};

export type GetConfigSourcesResponse = GetConfigSourcesResponses[keyof GetConfigSourcesResponses];

export type ConnectivityProbeData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/connectivity/probe';
};

export type ConnectivityProbeErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ConnectivityProbeError = ConnectivityProbeErrors[keyof ConnectivityProbeErrors];

export type ConnectivityProbeResponses = {
    /**
     * OK
     */
    200: ProbeBody;
};

export type ConnectivityProbeResponse = ConnectivityProbeResponses[keyof ConnectivityProbeResponses];

export type ApiDocsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/docs';
};

export type ApiDocsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ApiDocsError = ApiDocsErrors[keyof ApiDocsErrors];

export type ApiDocsResponses = {
    /**
     * HTML response
     */
    200: unknown;
};

export type ExtraStreamData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/extras/{id}/stream';
};

export type ExtraStreamErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ExtraStreamError = ExtraStreamErrors[keyof ExtraStreamErrors];

export type ExtraStreamResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type ExtraStreamResponse = ExtraStreamResponses[keyof ExtraStreamResponses];

export type ExtraThumbnailData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/extras/{id}/thumbnail';
};

export type ExtraThumbnailErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ExtraThumbnailError = ExtraThumbnailErrors[keyof ExtraThumbnailErrors];

export type ExtraThumbnailResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type ExtraThumbnailResponse = ExtraThumbnailResponses[keyof ExtraThumbnailResponses];

export type FsBrowseData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Absolute directory to list
         */
        path?: string;
    };
    url: '/api/fs/browse';
};

export type FsBrowseErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type FsBrowseError = FsBrowseErrors[keyof FsBrowseErrors];

export type FsBrowseResponses = {
    /**
     * OK
     */
    200: FsBrowseBody;
};

export type FsBrowseResponse = FsBrowseResponses[keyof FsBrowseResponses];

export type ListGenresData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/genres';
};

export type ListGenresErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListGenresError = ListGenresErrors[keyof ListGenresErrors];

export type ListGenresResponses = {
    /**
     * OK
     */
    200: Array<ListAllGenresRow> | null;
};

export type ListGenresResponse = ListGenresResponses[keyof ListGenresResponses];

export type GetGenreData = {
    body?: never;
    path: {
        /**
         * Exact genre/keyword name, URL-encoded; matched verbatim (dashes are literal, not space separators)
         */
        name: string;
    };
    query?: {
        /**
         * Restrict to one media type; empty = all
         */
        type?: 'movie' | 'tv' | 'anime' | 'book' | 'music' | 'comic';
        /**
         * Server-side sort — the browse grid is random-access paged
         */
        sort?: 'title' | 'year-desc' | 'year-asc';
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/genres/{name}';
};

export type GetGenreErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetGenreError = GetGenreErrors[keyof GetGenreErrors];

export type GetGenreResponses = {
    /**
     * OK
     */
    200: GenreResult;
};

export type GetGenreResponse = GetGenreResponses[keyof GetGenreResponses];

export type HealthData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/health';
};

export type HealthErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type HealthError = HealthErrors[keyof HealthErrors];

export type HealthResponses = {
    /**
     * OK
     */
    200: HealthBody;
};

export type HealthResponse = HealthResponses[keyof HealthResponses];

export type HealthLiveData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/health/live';
};

export type HealthLiveErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type HealthLiveError = HealthLiveErrors[keyof HealthLiveErrors];

export type HealthLiveResponses = {
    /**
     * OK
     */
    200: LiveBody;
};

export type HealthLiveResponse = HealthLiveResponses[keyof HealthLiveResponses];

export type HealthReadyData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/health/ready';
};

export type HealthReadyErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type HealthReadyError = HealthReadyErrors[keyof HealthReadyErrors];

export type HealthReadyResponses = {
    /**
     * OK
     */
    200: ReadyBody;
};

export type HealthReadyResponse = HealthReadyResponses[keyof HealthReadyResponses];

export type JellyfinConfigData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jellyfin/config';
};

export type JellyfinConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type JellyfinConfigError = JellyfinConfigErrors[keyof JellyfinConfigErrors];

export type JellyfinConfigResponses = {
    /**
     * OK
     */
    200: JellyfinConfigBody;
};

export type JellyfinConfigResponse = JellyfinConfigResponses[keyof JellyfinConfigResponses];

export type SetJellyfinConfigData = {
    body: JellyfinConfigBodyWritable;
    path?: never;
    query?: never;
    url: '/api/jellyfin/config';
};

export type SetJellyfinConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetJellyfinConfigError = SetJellyfinConfigErrors[keyof SetJellyfinConfigErrors];

export type SetJellyfinConfigResponses = {
    /**
     * OK
     */
    200: JellyfinConfigBody;
};

export type SetJellyfinConfigResponse = SetJellyfinConfigResponses[keyof SetJellyfinConfigResponses];

export type ClearAllJobsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jobs';
};

export type ClearAllJobsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ClearAllJobsError = ClearAllJobsErrors[keyof ClearAllJobsErrors];

export type ClearAllJobsResponses = {
    /**
     * OK
     */
    200: ClearedBody;
};

export type ClearAllJobsResponse = ClearAllJobsResponses[keyof ClearAllJobsResponses];

export type ListJobsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Filter by River state
         */
        state?: 'available' | 'running' | 'scheduled' | 'retryable' | 'completed' | 'cancelled' | 'discarded';
        /**
         * Filter by job kind (River task name)
         */
        kind?: string;
        limit?: number;
        /**
         * Return jobs with IDs lower than this cursor
         */
        before_id?: number;
    };
    url: '/api/jobs';
};

export type ListJobsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListJobsError = ListJobsErrors[keyof ListJobsErrors];

export type ListJobsResponses = {
    /**
     * OK
     */
    200: JobListResult;
};

export type ListJobsResponse = ListJobsResponses[keyof ListJobsResponses];

export type ClearJobsByKindData = {
    body?: never;
    path?: never;
    query: {
        /**
         * Job kind to flush (River task name)
         */
        kind: string;
        /**
         * Optional state to narrow the flush
         */
        state?: 'available' | 'running' | 'scheduled' | 'retryable' | 'completed' | 'cancelled' | 'discarded';
    };
    url: '/api/jobs/by-kind';
};

export type ClearJobsByKindErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ClearJobsByKindError = ClearJobsByKindErrors[keyof ClearJobsByKindErrors];

export type ClearJobsByKindResponses = {
    /**
     * OK
     */
    200: ClearedBody;
};

export type ClearJobsByKindResponse = ClearJobsByKindResponses[keyof ClearJobsByKindResponses];

export type ClearCompletedJobsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jobs/completed';
};

export type ClearCompletedJobsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ClearCompletedJobsError = ClearCompletedJobsErrors[keyof ClearCompletedJobsErrors];

export type ClearCompletedJobsResponses = {
    /**
     * OK
     */
    200: ClearedBody;
};

export type ClearCompletedJobsResponse = ClearCompletedJobsResponses[keyof ClearCompletedJobsResponses];

export type JobKindSummaryData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jobs/kinds';
};

export type JobKindSummaryErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type JobKindSummaryError = JobKindSummaryErrors[keyof JobKindSummaryErrors];

export type JobKindSummaryResponses = {
    /**
     * OK
     */
    200: Array<JobKindSummaryRow> | null;
};

export type JobKindSummaryResponse = JobKindSummaryResponses[keyof JobKindSummaryResponses];

export type MetadataQueueStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jobs/queue/metadata';
};

export type MetadataQueueStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MetadataQueueStatusError = MetadataQueueStatusErrors[keyof MetadataQueueStatusErrors];

export type MetadataQueueStatusResponses = {
    /**
     * OK
     */
    200: MetadataQueueStatus;
};

export type MetadataQueueStatusResponse = MetadataQueueStatusResponses[keyof MetadataQueueStatusResponses];

export type RescueJobsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jobs/rescue';
};

export type RescueJobsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RescueJobsError = RescueJobsErrors[keyof RescueJobsErrors];

export type RescueJobsResponses = {
    /**
     * OK
     */
    200: RescueBody;
};

export type RescueJobsResponse = RescueJobsResponses[keyof RescueJobsResponses];

export type JobSummaryData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jobs/summary';
};

export type JobSummaryErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type JobSummaryError = JobSummaryErrors[keyof JobSummaryErrors];

export type JobSummaryResponses = {
    /**
     * OK
     */
    200: Array<JobSummaryRow> | null;
};

export type JobSummaryResponse = JobSummaryResponses[keyof JobSummaryResponses];

export type JobWorkerSettingsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/jobs/worker-settings';
};

export type JobWorkerSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type JobWorkerSettingsError = JobWorkerSettingsErrors[keyof JobWorkerSettingsErrors];

export type JobWorkerSettingsResponses = {
    /**
     * OK
     */
    200: JobWorkerSettings;
};

export type JobWorkerSettingsResponse = JobWorkerSettingsResponses[keyof JobWorkerSettingsResponses];

export type SetJobWorkerSettingsData = {
    body: JobWorkerUpdateWritable;
    path?: never;
    query?: never;
    url: '/api/jobs/worker-settings';
};

export type SetJobWorkerSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetJobWorkerSettingsError = SetJobWorkerSettingsErrors[keyof SetJobWorkerSettingsErrors];

export type SetJobWorkerSettingsResponses = {
    /**
     * OK
     */
    200: StatusBody;
};

export type SetJobWorkerSettingsResponse = SetJobWorkerSettingsResponses[keyof SetJobWorkerSettingsResponses];

export type CancelJobData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/jobs/{id}/cancel';
};

export type CancelJobErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CancelJobError = CancelJobErrors[keyof CancelJobErrors];

export type CancelJobResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type CancelJobResponse = CancelJobResponses[keyof CancelJobResponses];

export type RetryJobData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/jobs/{id}/retry';
};

export type RetryJobErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RetryJobError = RetryJobErrors[keyof RetryJobErrors];

export type RetryJobResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RetryJobResponse = RetryJobResponses[keyof RetryJobResponses];

export type GetKeywordData = {
    body?: never;
    path: {
        /**
         * Exact genre/keyword name, URL-encoded; matched verbatim (dashes are literal, not space separators)
         */
        name: string;
    };
    query?: {
        /**
         * Restrict to one media type; empty = all
         */
        type?: 'movie' | 'tv' | 'anime' | 'book' | 'music' | 'comic';
        /**
         * Server-side sort — the browse grid is random-access paged
         */
        sort?: 'title' | 'year-desc' | 'year-asc';
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/keywords/{name}';
};

export type GetKeywordErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetKeywordError = GetKeywordErrors[keyof GetKeywordErrors];

export type GetKeywordResponses = {
    /**
     * OK
     */
    200: KeywordResult;
};

export type GetKeywordResponse = GetKeywordResponses[keyof GetKeywordResponses];

export type ListLibrariesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/libraries';
};

export type ListLibrariesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListLibrariesError = ListLibrariesErrors[keyof ListLibrariesErrors];

export type ListLibrariesResponses = {
    /**
     * OK
     */
    200: Array<LibraryView> | null;
};

export type ListLibrariesResponse = ListLibrariesResponses[keyof ListLibrariesResponses];

export type CreateLibraryData = {
    body: CreateLibraryInputBodyWritable;
    path?: never;
    query?: never;
    url: '/api/libraries';
};

export type CreateLibraryErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CreateLibraryError = CreateLibraryErrors[keyof CreateLibraryErrors];

export type CreateLibraryResponses = {
    /**
     * OK
     */
    200: LibraryView;
};

export type CreateLibraryResponse = CreateLibraryResponses[keyof CreateLibraryResponses];

export type CancelAllScansData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/libraries/scan/cancel-all';
};

export type CancelAllScansErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CancelAllScansError = CancelAllScansErrors[keyof CancelAllScansErrors];

export type CancelAllScansResponses = {
    /**
     * OK
     */
    200: CancelBody;
};

export type CancelAllScansResponse = CancelAllScansResponses[keyof CancelAllScansResponses];

export type DeleteLibraryData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}';
};

export type DeleteLibraryErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type DeleteLibraryError = DeleteLibraryErrors[keyof DeleteLibraryErrors];

export type DeleteLibraryResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type DeleteLibraryResponse = DeleteLibraryResponses[keyof DeleteLibraryResponses];

export type GetLibraryData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}';
};

export type GetLibraryErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetLibraryError = GetLibraryErrors[keyof GetLibraryErrors];

export type GetLibraryResponses = {
    /**
     * OK
     */
    200: LibraryView;
};

export type GetLibraryResponse = GetLibraryResponses[keyof GetLibraryResponses];

export type UpdateLibraryData = {
    body: UpdateLibraryRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}';
};

export type UpdateLibraryErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateLibraryError = UpdateLibraryErrors[keyof UpdateLibraryErrors];

export type UpdateLibraryResponses = {
    /**
     * OK
     */
    200: LibraryView;
};

export type UpdateLibraryResponse = UpdateLibraryResponses[keyof UpdateLibraryResponses];

export type ListLibraryFilesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/libraries/{id}/files';
};

export type ListLibraryFilesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListLibraryFilesError = ListLibraryFilesErrors[keyof ListLibraryFilesErrors];

export type ListLibraryFilesResponses = {
    /**
     * OK
     */
    200: Array<LibraryFile> | null;
};

export type ListLibraryFilesResponse = ListLibraryFilesResponses[keyof ListLibraryFilesResponses];

export type LibraryFileStatsData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/files/stats';
};

export type LibraryFileStatsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryFileStatsError = LibraryFileStatsErrors[keyof LibraryFileStatsErrors];

export type LibraryFileStatsResponses = {
    /**
     * OK
     */
    200: Array<CountLibraryFilesByStatusRow> | null;
};

export type LibraryFileStatsResponse = LibraryFileStatsResponses[keyof LibraryFileStatsResponses];

export type ListLibraryMediaData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Optional title filter
         */
        q?: string;
        limit?: number;
        offset?: number;
    };
    url: '/api/libraries/{id}/media';
};

export type ListLibraryMediaErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListLibraryMediaError = ListLibraryMediaErrors[keyof ListLibraryMediaErrors];

export type ListLibraryMediaResponses = {
    /**
     * OK
     */
    200: Array<MediaItemCard> | null;
};

export type ListLibraryMediaResponse = ListLibraryMediaResponses[keyof ListLibraryMediaResponses];

export type RefreshLibraryImagesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/refresh-images';
};

export type RefreshLibraryImagesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RefreshLibraryImagesError = RefreshLibraryImagesErrors[keyof RefreshLibraryImagesErrors];

export type RefreshLibraryImagesResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RefreshLibraryImagesResponse = RefreshLibraryImagesResponses[keyof RefreshLibraryImagesResponses];

export type RefreshLibraryMetadataData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/refresh-metadata';
};

export type RefreshLibraryMetadataErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RefreshLibraryMetadataError = RefreshLibraryMetadataErrors[keyof RefreshLibraryMetadataErrors];

export type RefreshLibraryMetadataResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RefreshLibraryMetadataResponse = RefreshLibraryMetadataResponses[keyof RefreshLibraryMetadataResponses];

export type ScanLibraryData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Force re-match of already-matched files
         */
        force?: boolean;
    };
    url: '/api/libraries/{id}/scan';
};

export type ScanLibraryErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ScanLibraryError = ScanLibraryErrors[keyof ScanLibraryErrors];

export type ScanLibraryResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type ScanLibraryResponse = ScanLibraryResponses[keyof ScanLibraryResponses];

export type CancelLibraryScanData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scan/cancel';
};

export type CancelLibraryScanErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CancelLibraryScanError = CancelLibraryScanErrors[keyof CancelLibraryScanErrors];

export type CancelLibraryScanResponses = {
    /**
     * OK
     */
    200: CancelBody;
};

export type CancelLibraryScanResponse = CancelLibraryScanResponses[keyof CancelLibraryScanResponses];

export type LibraryScannerBulkApproveSingleData = {
    body: LibraryScannerBulkApproveSingleRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/bulk-approve-single';
};

export type LibraryScannerBulkApproveSingleErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerBulkApproveSingleError = LibraryScannerBulkApproveSingleErrors[keyof LibraryScannerBulkApproveSingleErrors];

export type LibraryScannerBulkApproveSingleResponses = {
    /**
     * OK
     */
    200: ScannerBulkApproveResult;
};

export type LibraryScannerBulkApproveSingleResponse = LibraryScannerBulkApproveSingleResponses[keyof LibraryScannerBulkApproveSingleResponses];

export type LibraryScannerBulkEligibleData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        min_confidence?: number;
    };
    url: '/api/libraries/{id}/scanner/bulk-eligible';
};

export type LibraryScannerBulkEligibleErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerBulkEligibleError = LibraryScannerBulkEligibleErrors[keyof LibraryScannerBulkEligibleErrors];

export type LibraryScannerBulkEligibleResponses = {
    /**
     * OK
     */
    200: ScannerBulkEligibleResult;
};

export type LibraryScannerBulkEligibleResponse = LibraryScannerBulkEligibleResponses[keyof LibraryScannerBulkEligibleResponses];

export type LibraryScannerIdentitiesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
        /**
         * Filter by computed bucket: matched, needs_review, unmatched, rejected, ignored
         */
        bucket?: string;
        /**
         * Case-insensitive title / identity-key filter
         */
        q?: string;
    };
    url: '/api/libraries/{id}/scanner/identities';
};

export type LibraryScannerIdentitiesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerIdentitiesError = LibraryScannerIdentitiesErrors[keyof LibraryScannerIdentitiesErrors];

export type LibraryScannerIdentitiesResponses = {
    /**
     * OK
     */
    200: Array<ScannerIdentityView> | null;
};

export type LibraryScannerIdentitiesResponse = LibraryScannerIdentitiesResponses[keyof LibraryScannerIdentitiesResponses];

export type LibraryScannerIdentityData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}';
};

export type LibraryScannerIdentityErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerIdentityError = LibraryScannerIdentityErrors[keyof LibraryScannerIdentityErrors];

export type LibraryScannerIdentityResponses = {
    /**
     * OK
     */
    200: ScannerIdentityView;
};

export type LibraryScannerIdentityResponse = LibraryScannerIdentityResponses[keyof LibraryScannerIdentityResponses];

export type LibraryScannerApproveCandidateData = {
    body: LibraryScannerApproveCandidateRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/approve-candidate';
};

export type LibraryScannerApproveCandidateErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerApproveCandidateError = LibraryScannerApproveCandidateErrors[keyof LibraryScannerApproveCandidateErrors];

export type LibraryScannerApproveCandidateResponses = {
    /**
     * OK
     */
    200: ScannerIdentityView;
};

export type LibraryScannerApproveCandidateResponse = LibraryScannerApproveCandidateResponses[keyof LibraryScannerApproveCandidateResponses];

export type LibraryScannerAssignIdentityData = {
    body: LibraryScannerAssignIdentityRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/assign';
};

export type LibraryScannerAssignIdentityErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerAssignIdentityError = LibraryScannerAssignIdentityErrors[keyof LibraryScannerAssignIdentityErrors];

export type LibraryScannerAssignIdentityResponses = {
    /**
     * OK
     */
    200: ScannerIdentityView;
};

export type LibraryScannerAssignIdentityResponse = LibraryScannerAssignIdentityResponses[keyof LibraryScannerAssignIdentityResponses];

export type LibraryScannerIdentityCandidatesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/candidates';
};

export type LibraryScannerIdentityCandidatesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerIdentityCandidatesError = LibraryScannerIdentityCandidatesErrors[keyof LibraryScannerIdentityCandidatesErrors];

export type LibraryScannerIdentityCandidatesResponses = {
    /**
     * OK
     */
    200: Array<ScannerCandidateView> | null;
};

export type LibraryScannerIdentityCandidatesResponse = LibraryScannerIdentityCandidatesResponses[keyof LibraryScannerIdentityCandidatesResponses];

export type LibraryScannerCandidateDetailData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
        candidate_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/candidates/{candidate_id}/detail';
};

export type LibraryScannerCandidateDetailErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerCandidateDetailError = LibraryScannerCandidateDetailErrors[keyof LibraryScannerCandidateDetailErrors];

export type LibraryScannerCandidateDetailResponses = {
    /**
     * OK
     */
    200: ScannerCandidateDetailView;
};

export type LibraryScannerCandidateDetailResponse = LibraryScannerCandidateDetailResponses[keyof LibraryScannerCandidateDetailResponses];

export type LibraryScannerIdentityFindingsData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/findings';
};

export type LibraryScannerIdentityFindingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerIdentityFindingsError = LibraryScannerIdentityFindingsErrors[keyof LibraryScannerIdentityFindingsErrors];

export type LibraryScannerIdentityFindingsResponses = {
    /**
     * OK
     */
    200: Array<ScannerFindingView> | null;
};

export type LibraryScannerIdentityFindingsResponse = LibraryScannerIdentityFindingsResponses[keyof LibraryScannerIdentityFindingsResponses];

export type LibraryScannerIgnoreIdentityData = {
    body: LibraryScannerIgnoreIdentityRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/ignore';
};

export type LibraryScannerIgnoreIdentityErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerIgnoreIdentityError = LibraryScannerIgnoreIdentityErrors[keyof LibraryScannerIgnoreIdentityErrors];

export type LibraryScannerIgnoreIdentityResponses = {
    /**
     * OK
     */
    200: ScannerIdentityView;
};

export type LibraryScannerIgnoreIdentityResponse = LibraryScannerIgnoreIdentityResponses[keyof LibraryScannerIgnoreIdentityResponses];

export type LibraryScannerRejectIdentityData = {
    body: LibraryScannerRejectIdentityRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/reject';
};

export type LibraryScannerRejectIdentityErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerRejectIdentityError = LibraryScannerRejectIdentityErrors[keyof LibraryScannerRejectIdentityErrors];

export type LibraryScannerRejectIdentityResponses = {
    /**
     * OK
     */
    200: ScannerIdentityView;
};

export type LibraryScannerRejectIdentityResponse = LibraryScannerRejectIdentityResponses[keyof LibraryScannerRejectIdentityResponses];

export type LibraryScannerRematchIdentityData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/rematch';
};

export type LibraryScannerRematchIdentityErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerRematchIdentityError = LibraryScannerRematchIdentityErrors[keyof LibraryScannerRematchIdentityErrors];

export type LibraryScannerRematchIdentityResponses = {
    /**
     * OK
     */
    200: ScannerIdentityView;
};

export type LibraryScannerRematchIdentityResponse = LibraryScannerRematchIdentityResponses[keyof LibraryScannerRematchIdentityResponses];

export type LibraryScannerIdentitySearchData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        identity_id: number;
    };
    query?: {
        /**
         * Title query or provider URL/shortcode
         */
        q?: string;
        /**
         * Year hint (4-digit)
         */
        year?: string;
    };
    url: '/api/libraries/{id}/scanner/identities/{identity_id}/search';
};

export type LibraryScannerIdentitySearchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerIdentitySearchError = LibraryScannerIdentitySearchErrors[keyof LibraryScannerIdentitySearchErrors];

export type LibraryScannerIdentitySearchResponses = {
    /**
     * OK
     */
    200: IdentifySearchResult;
};

export type LibraryScannerIdentitySearchResponse = LibraryScannerIdentitySearchResponses[keyof LibraryScannerIdentitySearchResponses];

export type LibraryScannerIssuesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
        /**
         * Filter by finding code
         */
        code?: string;
    };
    url: '/api/libraries/{id}/scanner/issues';
};

export type LibraryScannerIssuesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerIssuesError = LibraryScannerIssuesErrors[keyof LibraryScannerIssuesErrors];

export type LibraryScannerIssuesResponses = {
    /**
     * OK
     */
    200: Array<ScannerFindingView> | null;
};

export type LibraryScannerIssuesResponse = LibraryScannerIssuesResponses[keyof LibraryScannerIssuesResponses];

export type LibraryScannerOverviewData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/scanner/overview';
};

export type LibraryScannerOverviewErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerOverviewError = LibraryScannerOverviewErrors[keyof LibraryScannerOverviewErrors];

export type LibraryScannerOverviewResponses = {
    /**
     * OK
     */
    200: ScannerOverview;
};

export type LibraryScannerOverviewResponse = LibraryScannerOverviewResponses[keyof LibraryScannerOverviewResponses];

export type LibraryScannerRunsData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/libraries/{id}/scanner/runs';
};

export type LibraryScannerRunsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LibraryScannerRunsError = LibraryScannerRunsErrors[keyof LibraryScannerRunsErrors];

export type LibraryScannerRunsResponses = {
    /**
     * OK
     */
    200: Array<ScannerRunView> | null;
};

export type LibraryScannerRunsResponse = LibraryScannerRunsResponses[keyof LibraryScannerRunsResponses];

export type GetLibrarySettingsData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Media type for default settings
         */
        type?: '' | 'movie' | 'tv' | 'music' | 'book' | 'comic' | 'podcast' | 'radio';
    };
    url: '/api/libraries/{id}/settings';
};

export type GetLibrarySettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetLibrarySettingsError = GetLibrarySettingsErrors[keyof GetLibrarySettingsErrors];

export type GetLibrarySettingsResponses = {
    /**
     * OK
     */
    200: LibrarySettingsBody;
};

export type GetLibrarySettingsResponse = GetLibrarySettingsResponses[keyof GetLibrarySettingsResponses];

export type UpdateLibrarySettingsData = {
    body: LibrarySettingsWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/settings';
};

export type UpdateLibrarySettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateLibrarySettingsError = UpdateLibrarySettingsErrors[keyof UpdateLibrarySettingsErrors];

export type UpdateLibrarySettingsResponses = {
    /**
     * OK
     */
    200: LibraryView;
};

export type UpdateLibrarySettingsResponse = UpdateLibrarySettingsResponses[keyof UpdateLibrarySettingsResponses];

export type ListUnmatchedData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/libraries/{id}/unmatched';
};

export type ListUnmatchedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListUnmatchedError = ListUnmatchedErrors[keyof ListUnmatchedErrors];

export type ListUnmatchedResponses = {
    /**
     * OK
     */
    200: Array<UnmatchedFile> | null;
};

export type ListUnmatchedResponse = ListUnmatchedResponses[keyof ListUnmatchedResponses];

export type ResolveMatchData = {
    body: ResolveMatchRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/library-files/{id}/resolve';
};

export type ResolveMatchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ResolveMatchError = ResolveMatchErrors[keyof ResolveMatchErrors];

export type ResolveMatchResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type ResolveMatchResponse = ResolveMatchResponses[keyof ResolveMatchResponses];

export type GetLogsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Number of entries
         */
        n?: number;
        /**
         * Filter by log level (trace|debug|info|warn|error)
         */
        level?: string;
    };
    url: '/api/logs';
};

export type GetLogsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetLogsError = GetLogsErrors[keyof GetLogsErrors];

export type GetLogsResponses = {
    /**
     * OK
     */
    200: Array<Entry> | null;
};

export type GetLogsResponse = GetLogsResponses[keyof GetLogsResponses];

export type ListApiTokensData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/api-tokens';
};

export type ListApiTokensErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListApiTokensError = ListApiTokensErrors[keyof ListApiTokensErrors];

export type ListApiTokensResponses = {
    /**
     * OK
     */
    200: Array<ApiTokenView> | null;
};

export type ListApiTokensResponse = ListApiTokensResponses[keyof ListApiTokensResponses];

export type CreateApiTokenData = {
    body: CreateApiTokenRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/api-tokens';
};

export type CreateApiTokenErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CreateApiTokenError = CreateApiTokenErrors[keyof CreateApiTokenErrors];

export type CreateApiTokenResponses = {
    /**
     * OK
     */
    200: CreateApiTokenResult;
};

export type CreateApiTokenResponse = CreateApiTokenResponses[keyof CreateApiTokenResponses];

export type RevokeApiTokenData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/api-tokens/{id}';
};

export type RevokeApiTokenErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RevokeApiTokenError = RevokeApiTokenErrors[keyof RevokeApiTokenErrors];

export type RevokeApiTokenResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type RevokeApiTokenResponse = RevokeApiTokenResponses[keyof RevokeApiTokenResponses];

export type ListAuthSessionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/auth-sessions';
};

export type ListAuthSessionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListAuthSessionsError = ListAuthSessionsErrors[keyof ListAuthSessionsErrors];

export type ListAuthSessionsResponses = {
    /**
     * OK
     */
    200: Array<AuthSessionView> | null;
};

export type ListAuthSessionsResponse = ListAuthSessionsResponses[keyof ListAuthSessionsResponses];

export type RevokeOtherAuthSessionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/auth-sessions/revoke-others';
};

export type RevokeOtherAuthSessionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RevokeOtherAuthSessionsError = RevokeOtherAuthSessionsErrors[keyof RevokeOtherAuthSessionsErrors];

export type RevokeOtherAuthSessionsResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type RevokeOtherAuthSessionsResponse = RevokeOtherAuthSessionsResponses[keyof RevokeOtherAuthSessionsResponses];

export type RevokeAuthSessionData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/auth-sessions/{id}';
};

export type RevokeAuthSessionErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RevokeAuthSessionError = RevokeAuthSessionErrors[keyof RevokeAuthSessionErrors];

export type RevokeAuthSessionResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type RevokeAuthSessionResponse = RevokeAuthSessionResponses[keyof RevokeAuthSessionResponses];

export type ToggleFavoriteData = {
    body: ToggleFavoriteRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/favorites';
};

export type ToggleFavoriteErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ToggleFavoriteError = ToggleFavoriteErrors[keyof ToggleFavoriteErrors];

export type ToggleFavoriteResponses = {
    /**
     * OK
     */
    200: FavoritedBody;
};

export type ToggleFavoriteResponse = ToggleFavoriteResponses[keyof ToggleFavoriteResponses];

export type CheckFavoriteData = {
    body?: never;
    path?: never;
    query?: {
        entity_type?: 'media_item' | 'episode' | 'season' | 'track' | 'artist' | 'album';
        entity_id?: number;
    };
    url: '/api/me/favorites/check';
};

export type CheckFavoriteErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CheckFavoriteError = CheckFavoriteErrors[keyof CheckFavoriteErrors];

export type CheckFavoriteResponses = {
    /**
     * OK
     */
    200: FavoritedBody;
};

export type CheckFavoriteResponse = CheckFavoriteResponses[keyof CheckFavoriteResponses];

export type RevokeJellyfinCredentialData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/jellyfin-credential';
};

export type RevokeJellyfinCredentialErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RevokeJellyfinCredentialError = RevokeJellyfinCredentialErrors[keyof RevokeJellyfinCredentialErrors];

export type RevokeJellyfinCredentialResponses = {
    /**
     * No Content
     */
    204: void;
};

export type RevokeJellyfinCredentialResponse = RevokeJellyfinCredentialResponses[keyof RevokeJellyfinCredentialResponses];

export type GetJellyfinCredentialData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/jellyfin-credential';
};

export type GetJellyfinCredentialErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetJellyfinCredentialError = GetJellyfinCredentialErrors[keyof GetJellyfinCredentialErrors];

export type GetJellyfinCredentialResponses = {
    /**
     * OK
     */
    200: JellyfinCredentialBody;
};

export type GetJellyfinCredentialResponse = GetJellyfinCredentialResponses[keyof GetJellyfinCredentialResponses];

export type RotateJellyfinCredentialData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/jellyfin-credential';
};

export type RotateJellyfinCredentialErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RotateJellyfinCredentialError = RotateJellyfinCredentialErrors[keyof RotateJellyfinCredentialErrors];

export type RotateJellyfinCredentialResponses = {
    /**
     * OK
     */
    200: JellyfinCredentialBody;
};

export type RotateJellyfinCredentialResponse = RotateJellyfinCredentialResponses[keyof RotateJellyfinCredentialResponses];

export type GetListeningStatsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/listening-stats';
};

export type GetListeningStatsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetListeningStatsError = GetListeningStatsErrors[keyof GetListeningStatsErrors];

export type GetListeningStatsResponses = {
    /**
     * OK
     */
    200: ListeningStats;
};

export type GetListeningStatsResponse = GetListeningStatsResponses[keyof GetListeningStatsResponses];

export type ListUserListsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * When set, returns lists with a contains flag for this item
         */
        media_item_id?: number;
    };
    url: '/api/me/lists';
};

export type ListUserListsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListUserListsError = ListUserListsErrors[keyof ListUserListsErrors];

export type ListUserListsResponses = {
    /**
     * OK
     */
    200: Array<UserListView> | null;
};

export type ListUserListsResponse = ListUserListsResponses[keyof ListUserListsResponses];

export type CreateUserListData = {
    body: CreateUserListRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/lists';
};

export type CreateUserListErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CreateUserListError = CreateUserListErrors[keyof CreateUserListErrors];

export type CreateUserListResponses = {
    /**
     * OK
     */
    200: UserListView;
};

export type CreateUserListResponse = CreateUserListResponses[keyof CreateUserListResponses];

export type DeleteUserListData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/lists/{id}';
};

export type DeleteUserListErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type DeleteUserListError = DeleteUserListErrors[keyof DeleteUserListErrors];

export type DeleteUserListResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type DeleteUserListResponse = DeleteUserListResponses[keyof DeleteUserListResponses];

export type GetUserListData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/lists/{id}';
};

export type GetUserListErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetUserListError = GetUserListErrors[keyof GetUserListErrors];

export type GetUserListResponses = {
    /**
     * OK
     */
    200: UserListDetailBody;
};

export type GetUserListResponse = GetUserListResponses[keyof GetUserListResponses];

export type UpdateUserListData = {
    body: UpdateUserListRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/lists/{id}';
};

export type UpdateUserListErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateUserListError = UpdateUserListErrors[keyof UpdateUserListErrors];

export type UpdateUserListResponses = {
    /**
     * OK
     */
    200: UserListView;
};

export type UpdateUserListResponse = UpdateUserListResponses[keyof UpdateUserListResponses];

export type AddListItemData = {
    body: AddListItemRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/lists/{id}/items';
};

export type AddListItemErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AddListItemError = AddListItemErrors[keyof AddListItemErrors];

export type AddListItemResponses = {
    /**
     * OK
     */
    200: UserListItem;
};

export type AddListItemResponse = AddListItemResponses[keyof AddListItemResponses];

export type RemoveListItemData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        media_id: number;
    };
    query?: never;
    url: '/api/me/lists/{id}/items/{media_id}';
};

export type RemoveListItemErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RemoveListItemError = RemoveListItemErrors[keyof RemoveListItemErrors];

export type RemoveListItemResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RemoveListItemResponse = RemoveListItemResponses[keyof RemoveListItemResponses];

export type ReorderListData = {
    body: ReorderListRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/lists/{id}/reorder';
};

export type ReorderListErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ReorderListError = ReorderListErrors[keyof ReorderListErrors];

export type ReorderListResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type ReorderListResponse = ReorderListResponses[keyof ReorderListResponses];

export type ListLovedAlbumsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/me/loved/albums';
};

export type ListLovedAlbumsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListLovedAlbumsError = ListLovedAlbumsErrors[keyof ListLovedAlbumsErrors];

export type ListLovedAlbumsResponses = {
    /**
     * OK
     */
    200: MusicListPageListUserLovedAlbumsRow;
};

export type ListLovedAlbumsResponse = ListLovedAlbumsResponses[keyof ListLovedAlbumsResponses];

export type LovedAlbumIdsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/loved/albums/ids';
};

export type LovedAlbumIdsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LovedAlbumIdsError = LovedAlbumIdsErrors[keyof LovedAlbumIdsErrors];

export type LovedAlbumIdsResponses = {
    /**
     * OK
     */
    200: IdsBody;
};

export type LovedAlbumIdsResponse = LovedAlbumIdsResponses[keyof LovedAlbumIdsResponses];

export type UnloveAlbumData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/loved/albums/{id}';
};

export type UnloveAlbumErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UnloveAlbumError = UnloveAlbumErrors[keyof UnloveAlbumErrors];

export type UnloveAlbumResponses = {
    /**
     * OK
     */
    200: LovedBody;
};

export type UnloveAlbumResponse = UnloveAlbumResponses[keyof UnloveAlbumResponses];

export type LoveAlbumData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/loved/albums/{id}';
};

export type LoveAlbumErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LoveAlbumError = LoveAlbumErrors[keyof LoveAlbumErrors];

export type LoveAlbumResponses = {
    /**
     * OK
     */
    200: LovedBody;
};

export type LoveAlbumResponse = LoveAlbumResponses[keyof LoveAlbumResponses];

export type ListLovedArtistsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/me/loved/artists';
};

export type ListLovedArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListLovedArtistsError = ListLovedArtistsErrors[keyof ListLovedArtistsErrors];

export type ListLovedArtistsResponses = {
    /**
     * OK
     */
    200: MusicListPageListUserLovedArtistsRow;
};

export type ListLovedArtistsResponse = ListLovedArtistsResponses[keyof ListLovedArtistsResponses];

export type LovedArtistIdsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/loved/artists/ids';
};

export type LovedArtistIdsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LovedArtistIdsError = LovedArtistIdsErrors[keyof LovedArtistIdsErrors];

export type LovedArtistIdsResponses = {
    /**
     * OK
     */
    200: IdsBody;
};

export type LovedArtistIdsResponse = LovedArtistIdsResponses[keyof LovedArtistIdsResponses];

export type UnloveArtistData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/loved/artists/{id}';
};

export type UnloveArtistErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UnloveArtistError = UnloveArtistErrors[keyof UnloveArtistErrors];

export type UnloveArtistResponses = {
    /**
     * OK
     */
    200: LovedBody;
};

export type UnloveArtistResponse = UnloveArtistResponses[keyof UnloveArtistResponses];

export type LoveArtistData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/loved/artists/{id}';
};

export type LoveArtistErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LoveArtistError = LoveArtistErrors[keyof LoveArtistErrors];

export type LoveArtistResponses = {
    /**
     * OK
     */
    200: LovedBody;
};

export type LoveArtistResponse = LoveArtistResponses[keyof LoveArtistResponses];

export type ListLovedTracksData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/me/loved/tracks';
};

export type ListLovedTracksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListLovedTracksError = ListLovedTracksErrors[keyof ListLovedTracksErrors];

export type ListLovedTracksResponses = {
    /**
     * OK
     */
    200: MusicListPageListUserLovedTracksRow;
};

export type ListLovedTracksResponse = ListLovedTracksResponses[keyof ListLovedTracksResponses];

export type LovedTrackIdsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/loved/tracks/ids';
};

export type LovedTrackIdsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LovedTrackIdsError = LovedTrackIdsErrors[keyof LovedTrackIdsErrors];

export type LovedTrackIdsResponses = {
    /**
     * OK
     */
    200: IdsBody;
};

export type LovedTrackIdsResponse = LovedTrackIdsResponses[keyof LovedTrackIdsResponses];

export type UnloveTrackData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/loved/tracks/{id}';
};

export type UnloveTrackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UnloveTrackError = UnloveTrackErrors[keyof UnloveTrackErrors];

export type UnloveTrackResponses = {
    /**
     * OK
     */
    200: LovedBody;
};

export type UnloveTrackResponse = UnloveTrackResponses[keyof UnloveTrackResponses];

export type LoveTrackData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/loved/tracks/{id}';
};

export type LoveTrackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LoveTrackError = LoveTrackErrors[keyof LoveTrackErrors];

export type LoveTrackResponses = {
    /**
     * OK
     */
    200: LovedBody;
};

export type LoveTrackResponse = LoveTrackResponses[keyof LoveTrackResponses];

export type GetMediaStateData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/media-state';
};

export type GetMediaStateErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetMediaStateError = GetMediaStateErrors[keyof GetMediaStateErrors];

export type GetMediaStateResponses = {
    /**
     * OK
     */
    200: MediaStateBody;
};

export type GetMediaStateResponse = GetMediaStateResponses[keyof GetMediaStateResponses];

export type ListMusicServicesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/music-services';
};

export type ListMusicServicesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListMusicServicesError = ListMusicServicesErrors[keyof ListMusicServicesErrors];

export type ListMusicServicesResponses = {
    /**
     * OK
     */
    200: MusicServicesBody;
};

export type ListMusicServicesResponse = ListMusicServicesResponses[keyof ListMusicServicesResponses];

export type LastfmAuthCompleteData = {
    body: LastfmAuthCompleteRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/music-services/lastfm/auth-complete';
};

export type LastfmAuthCompleteErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LastfmAuthCompleteError = LastfmAuthCompleteErrors[keyof LastfmAuthCompleteErrors];

export type LastfmAuthCompleteResponses = {
    /**
     * OK
     */
    200: MusicServiceView;
};

export type LastfmAuthCompleteResponse = LastfmAuthCompleteResponses[keyof LastfmAuthCompleteResponses];

export type LastfmAuthStartData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/music-services/lastfm/auth-start';
};

export type LastfmAuthStartErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type LastfmAuthStartError = LastfmAuthStartErrors[keyof LastfmAuthStartErrors];

export type LastfmAuthStartResponses = {
    /**
     * OK
     */
    200: LastfmAuthStartBody;
};

export type LastfmAuthStartResponse = LastfmAuthStartResponses[keyof LastfmAuthStartResponses];

export type SetMusicServiceData = {
    body: MusicServiceUpdateWritable;
    path: {
        service: 'listenbrainz' | 'lastfm';
    };
    query?: never;
    url: '/api/me/music-services/{service}';
};

export type SetMusicServiceErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetMusicServiceError = SetMusicServiceErrors[keyof SetMusicServiceErrors];

export type SetMusicServiceResponses = {
    /**
     * OK
     */
    200: MusicServiceView;
};

export type SetMusicServiceResponse = SetMusicServiceResponses[keyof SetMusicServiceResponses];

export type StartListenImportData = {
    body?: never;
    path: {
        service: 'listenbrainz' | 'lastfm';
    };
    query?: never;
    url: '/api/me/music-services/{service}/import';
};

export type StartListenImportErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StartListenImportError = StartListenImportErrors[keyof StartListenImportErrors];

export type StartListenImportResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type StartListenImportResponse = StartListenImportResponses[keyof StartListenImportResponses];

export type SetPlaylistCollectionPolicyData = {
    body: PlaylistSyncToggleWritable;
    path: {
        service: 'listenbrainz' | 'lastfm';
        collection: string;
    };
    query?: never;
    url: '/api/me/music-services/{service}/playlist-collections/{collection}';
};

export type SetPlaylistCollectionPolicyErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetPlaylistCollectionPolicyError = SetPlaylistCollectionPolicyErrors[keyof SetPlaylistCollectionPolicyErrors];

export type SetPlaylistCollectionPolicyResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type SetPlaylistCollectionPolicyResponse = SetPlaylistCollectionPolicyResponses[keyof SetPlaylistCollectionPolicyResponses];

export type ListExternalPlaylistsData = {
    body?: never;
    path: {
        service: 'listenbrainz' | 'lastfm';
    };
    query?: never;
    url: '/api/me/music-services/{service}/playlists';
};

export type ListExternalPlaylistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListExternalPlaylistsError = ListExternalPlaylistsErrors[keyof ListExternalPlaylistsErrors];

export type ListExternalPlaylistsResponses = {
    /**
     * OK
     */
    200: PlaylistServiceCatalog;
};

export type ListExternalPlaylistsResponse = ListExternalPlaylistsResponses[keyof ListExternalPlaylistsResponses];

export type SetExternalPlaylistSyncData = {
    body: PlaylistSyncToggleWritable;
    path: {
        service: 'listenbrainz' | 'lastfm';
        external_id: string;
    };
    query?: never;
    url: '/api/me/music-services/{service}/playlists/{external_id}/sync';
};

export type SetExternalPlaylistSyncErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetExternalPlaylistSyncError = SetExternalPlaylistSyncErrors[keyof SetExternalPlaylistSyncErrors];

export type SetExternalPlaylistSyncResponses = {
    /**
     * OK
     */
    200: ExternalPlaylistSyncBody;
};

export type SetExternalPlaylistSyncResponse = SetExternalPlaylistSyncResponses[keyof SetExternalPlaylistSyncResponses];

export type SyncReactionsOutData = {
    body?: never;
    path: {
        service: 'listenbrainz' | 'lastfm';
    };
    query?: never;
    url: '/api/me/music-services/{service}/sync-reactions';
};

export type SyncReactionsOutErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SyncReactionsOutError = SyncReactionsOutErrors[keyof SyncReactionsOutErrors];

export type SyncReactionsOutResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type SyncReactionsOutResponse = SyncReactionsOutResponses[keyof SyncReactionsOutResponses];

export type ChangePasswordData = {
    body: ChangePasswordRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/password';
};

export type ChangePasswordErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ChangePasswordError = ChangePasswordErrors[keyof ChangePasswordErrors];

export type ChangePasswordResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type ChangePasswordResponse = ChangePasswordResponses[keyof ChangePasswordResponses];

export type RecordPlaybackData = {
    body: PlaybackEventWritable;
    path?: never;
    query?: never;
    url: '/api/me/playback';
};

export type RecordPlaybackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecordPlaybackError = RecordPlaybackErrors[keyof RecordPlaybackErrors];

export type RecordPlaybackResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type RecordPlaybackResponse = RecordPlaybackResponses[keyof RecordPlaybackResponses];

export type GetPlaybackPreferenceData = {
    body?: never;
    path: {
        media_id: number;
    };
    query?: never;
    url: '/api/me/playback/{media_id}';
};

export type GetPlaybackPreferenceErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetPlaybackPreferenceError = GetPlaybackPreferenceErrors[keyof GetPlaybackPreferenceErrors];

export type GetPlaybackPreferenceResponses = {
    /**
     * OK
     */
    200: PlaybackPrefBody;
};

export type GetPlaybackPreferenceResponse = GetPlaybackPreferenceResponses[keyof GetPlaybackPreferenceResponses];

export type SetPlaybackPreferenceData = {
    body: SetPlaybackPreferenceRequestWritable;
    path: {
        media_id: number;
    };
    query?: never;
    url: '/api/me/playback/{media_id}';
};

export type SetPlaybackPreferenceErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetPlaybackPreferenceError = SetPlaybackPreferenceErrors[keyof SetPlaybackPreferenceErrors];

export type SetPlaybackPreferenceResponses = {
    /**
     * OK
     */
    200: PlaybackPrefBody;
};

export type SetPlaybackPreferenceResponse = SetPlaybackPreferenceResponses[keyof SetPlaybackPreferenceResponses];

export type ListPlaylistsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/playlists';
};

export type ListPlaylistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListPlaylistsError = ListPlaylistsErrors[keyof ListPlaylistsErrors];

export type ListPlaylistsResponses = {
    /**
     * OK
     */
    200: PlaylistsListBody;
};

export type ListPlaylistsResponse = ListPlaylistsResponses[keyof ListPlaylistsResponses];

export type CreatePlaylistData = {
    body: PlaylistMutationWritable;
    path?: never;
    query?: never;
    url: '/api/me/playlists';
};

export type CreatePlaylistErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CreatePlaylistError = CreatePlaylistErrors[keyof CreatePlaylistErrors];

export type CreatePlaylistResponses = {
    /**
     * OK
     */
    200: UserPlaylist;
};

export type CreatePlaylistResponse = CreatePlaylistResponses[keyof CreatePlaylistResponses];

export type SetPlaylistSidebarOrderData = {
    body: SetPlaylistSidebarOrderRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/playlists/sidebar-order';
};

export type SetPlaylistSidebarOrderErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetPlaylistSidebarOrderError = SetPlaylistSidebarOrderErrors[keyof SetPlaylistSidebarOrderErrors];

export type SetPlaylistSidebarOrderResponses = {
    /**
     * No Content
     */
    204: void;
};

export type SetPlaylistSidebarOrderResponse = SetPlaylistSidebarOrderResponses[keyof SetPlaylistSidebarOrderResponses];

export type DeletePlaylistData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}';
};

export type DeletePlaylistErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type DeletePlaylistError = DeletePlaylistErrors[keyof DeletePlaylistErrors];

export type DeletePlaylistResponses = {
    /**
     * No Content
     */
    204: void;
};

export type DeletePlaylistResponse = DeletePlaylistResponses[keyof DeletePlaylistResponses];

export type GetPlaylistData = {
    body?: never;
    path: {
        /**
         * Numeric ID or slug
         */
        id: string;
    };
    query?: never;
    url: '/api/me/playlists/{id}';
};

export type GetPlaylistErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetPlaylistError = GetPlaylistErrors[keyof GetPlaylistErrors];

export type GetPlaylistResponses = {
    /**
     * OK
     */
    200: PlaylistDetail;
};

export type GetPlaylistResponse = GetPlaylistResponses[keyof GetPlaylistResponses];

export type UpdatePlaylistData = {
    body: PlaylistMutationWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}';
};

export type UpdatePlaylistErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdatePlaylistError = UpdatePlaylistErrors[keyof UpdatePlaylistErrors];

export type UpdatePlaylistResponses = {
    /**
     * No Content
     */
    204: void;
};

export type UpdatePlaylistResponse = UpdatePlaylistResponses[keyof UpdatePlaylistResponses];

export type ClearPlaylistCoverData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}/cover';
};

export type ClearPlaylistCoverErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ClearPlaylistCoverError = ClearPlaylistCoverErrors[keyof ClearPlaylistCoverErrors];

export type ClearPlaylistCoverResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type ClearPlaylistCoverResponse = ClearPlaylistCoverResponses[keyof ClearPlaylistCoverResponses];

export type PlaylistCoverData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}/cover';
};

export type PlaylistCoverErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PlaylistCoverError = PlaylistCoverErrors[keyof PlaylistCoverErrors];

export type PlaylistCoverResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type PlaylistCoverResponse = PlaylistCoverResponses[keyof PlaylistCoverResponses];

export type UploadPlaylistCoverData = {
    body?: {
        file: Blob | File;
    };
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}/cover';
};

export type UploadPlaylistCoverErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UploadPlaylistCoverError = UploadPlaylistCoverErrors[keyof UploadPlaylistCoverErrors];

export type UploadPlaylistCoverResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type UploadPlaylistCoverResponse = UploadPlaylistCoverResponses[keyof UploadPlaylistCoverResponses];

export type SetPlaylistPinData = {
    body: SetPlaylistPinRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}/pin';
};

export type SetPlaylistPinErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetPlaylistPinError = SetPlaylistPinErrors[keyof SetPlaylistPinErrors];

export type SetPlaylistPinResponses = {
    /**
     * No Content
     */
    204: void;
};

export type SetPlaylistPinResponse = SetPlaylistPinResponses[keyof SetPlaylistPinResponses];

export type SyncPlaylistNowData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        service: 'listenbrainz' | 'lastfm';
    };
    query?: never;
    url: '/api/me/playlists/{id}/sync/{service}';
};

export type SyncPlaylistNowErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SyncPlaylistNowError = SyncPlaylistNowErrors[keyof SyncPlaylistNowErrors];

export type SyncPlaylistNowResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type SyncPlaylistNowResponse = SyncPlaylistNowResponses[keyof SyncPlaylistNowResponses];

export type SetLocalPlaylistSyncData = {
    body: PlaylistSyncToggleWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        service: 'listenbrainz' | 'lastfm';
    };
    query?: never;
    url: '/api/me/playlists/{id}/sync/{service}';
};

export type SetLocalPlaylistSyncErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetLocalPlaylistSyncError = SetLocalPlaylistSyncErrors[keyof SetLocalPlaylistSyncErrors];

export type SetLocalPlaylistSyncResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type SetLocalPlaylistSyncResponse = SetLocalPlaylistSyncResponses[keyof SetLocalPlaylistSyncResponses];

export type RemovePlaylistTrackData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        track_id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}/tracks/{track_id}';
};

export type RemovePlaylistTrackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RemovePlaylistTrackError = RemovePlaylistTrackErrors[keyof RemovePlaylistTrackErrors];

export type RemovePlaylistTrackResponses = {
    /**
     * No Content
     */
    204: void;
};

export type RemovePlaylistTrackResponse = RemovePlaylistTrackResponses[keyof RemovePlaylistTrackResponses];

export type AddPlaylistTrackData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        track_id: number;
    };
    query?: never;
    url: '/api/me/playlists/{id}/tracks/{track_id}';
};

export type AddPlaylistTrackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AddPlaylistTrackError = AddPlaylistTrackErrors[keyof AddPlaylistTrackErrors];

export type AddPlaylistTrackResponses = {
    /**
     * No Content
     */
    204: void;
};

export type AddPlaylistTrackResponse = AddPlaylistTrackResponses[keyof AddPlaylistTrackResponses];

export type PodcastsContinueData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/me/podcasts/continue';
};

export type PodcastsContinueErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PodcastsContinueError = PodcastsContinueErrors[keyof PodcastsContinueErrors];

export type PodcastsContinueResponses = {
    /**
     * OK
     */
    200: PodcastContinueBody;
};

export type PodcastsContinueResponse = PodcastsContinueResponses[keyof PodcastsContinueResponses];

export type RecordPodcastProgressData = {
    body: PodcastProgressInputWritable;
    path?: never;
    query?: never;
    url: '/api/me/podcasts/progress';
};

export type RecordPodcastProgressErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecordPodcastProgressError = RecordPodcastProgressErrors[keyof RecordPodcastProgressErrors];

export type RecordPodcastProgressResponses = {
    /**
     * OK
     */
    200: UserPodcastProgress;
};

export type RecordPodcastProgressResponse = RecordPodcastProgressResponses[keyof RecordPodcastProgressResponses];

export type UnsubscribePodcastData = {
    body?: never;
    path?: never;
    query?: {
        url?: string;
    };
    url: '/api/me/podcasts/subscriptions';
};

export type UnsubscribePodcastErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UnsubscribePodcastError = UnsubscribePodcastErrors[keyof UnsubscribePodcastErrors];

export type UnsubscribePodcastResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type UnsubscribePodcastResponse = UnsubscribePodcastResponses[keyof UnsubscribePodcastResponses];

export type ListPodcastSubscriptionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/podcasts/subscriptions';
};

export type ListPodcastSubscriptionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListPodcastSubscriptionsError = ListPodcastSubscriptionsErrors[keyof ListPodcastSubscriptionsErrors];

export type ListPodcastSubscriptionsResponses = {
    /**
     * OK
     */
    200: PodcastSubsBody;
};

export type ListPodcastSubscriptionsResponse = ListPodcastSubscriptionsResponses[keyof ListPodcastSubscriptionsResponses];

export type SubscribePodcastData = {
    body: SubscribePodcastRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/podcasts/subscriptions';
};

export type SubscribePodcastErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SubscribePodcastError = SubscribePodcastErrors[keyof SubscribePodcastErrors];

export type SubscribePodcastResponses = {
    /**
     * OK
     */
    200: UserPodcastSubscription;
};

export type SubscribePodcastResponse = SubscribePodcastResponses[keyof SubscribePodcastResponses];

export type QueueClearData = {
    body?: never;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue';
};

export type QueueClearErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueClearError = QueueClearErrors[keyof QueueClearErrors];

export type QueueClearResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueClearResponse = QueueClearResponses[keyof QueueClearResponses];

export type QueueGetData = {
    body?: never;
    path?: never;
    query?: {
        device_id?: string;
        /**
         * Window anchor ord; 0 anchors on the current item
         */
        around?: number;
        /**
         * Window size (default 100, max 500)
         */
        limit?: number;
    };
    url: '/api/me/queue';
};

export type QueueGetErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueGetError = QueueGetErrors[keyof QueueGetErrors];

export type QueueGetResponses = {
    /**
     * OK
     */
    200: QueueView;
};

export type QueueGetResponse = QueueGetResponses[keyof QueueGetResponses];

export type QueueReplaceData = {
    body: QueueReplaceRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue';
};

export type QueueReplaceErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueReplaceError = QueueReplaceErrors[keyof QueueReplaceErrors];

export type QueueReplaceResponses = {
    /**
     * OK
     */
    200: QueueView;
};

export type QueueReplaceResponse = QueueReplaceResponses[keyof QueueReplaceResponses];

export type QueueAdvanceData = {
    body: QueueAdvanceRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/advance';
};

export type QueueAdvanceErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueAdvanceError = QueueAdvanceErrors[keyof QueueAdvanceErrors];

export type QueueAdvanceResponses = {
    /**
     * OK
     */
    200: QueueView;
};

export type QueueAdvanceResponse = QueueAdvanceResponses[keyof QueueAdvanceResponses];

export type QueueClaimData = {
    body: QueueClaimRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/claim';
};

export type QueueClaimErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueClaimError = QueueClaimErrors[keyof QueueClaimErrors];

export type QueueClaimResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueClaimResponse = QueueClaimResponses[keyof QueueClaimResponses];

export type QueueHeartbeatData = {
    body: QueueHeartbeatRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/heartbeat';
};

export type QueueHeartbeatErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueHeartbeatError = QueueHeartbeatErrors[keyof QueueHeartbeatErrors];

export type QueueHeartbeatResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueHeartbeatResponse = QueueHeartbeatResponses[keyof QueueHeartbeatResponses];

export type QueueEnqueueData = {
    body: QueueEnqueueRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/items';
};

export type QueueEnqueueErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueEnqueueError = QueueEnqueueErrors[keyof QueueEnqueueErrors];

export type QueueEnqueueResponses = {
    /**
     * OK
     */
    200: AddedBody;
};

export type QueueEnqueueResponse = QueueEnqueueResponses[keyof QueueEnqueueResponses];

export type QueueRemoveItemData = {
    body?: never;
    path: {
        id: number;
    };
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/items/{id}';
};

export type QueueRemoveItemErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueRemoveItemError = QueueRemoveItemErrors[keyof QueueRemoveItemErrors];

export type QueueRemoveItemResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueRemoveItemResponse = QueueRemoveItemResponses[keyof QueueRemoveItemResponses];

export type QueueMoveItemData = {
    body: QueueMoveItemRequestWritable;
    path: {
        id: number;
    };
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/items/{id}/move';
};

export type QueueMoveItemErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueMoveItemError = QueueMoveItemErrors[keyof QueueMoveItemErrors];

export type QueueMoveItemResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueMoveItemResponse = QueueMoveItemResponses[keyof QueueMoveItemResponses];

export type QueueJumpData = {
    body: QueueJumpRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/jump';
};

export type QueueJumpErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueJumpError = QueueJumpErrors[keyof QueueJumpErrors];

export type QueueJumpResponses = {
    /**
     * OK
     */
    200: QueueView;
};

export type QueueJumpResponse = QueueJumpResponses[keyof QueueJumpResponses];

export type QueueRepeatData = {
    body: QueueRepeatRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/repeat';
};

export type QueueRepeatErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueRepeatError = QueueRepeatErrors[keyof QueueRepeatErrors];

export type QueueRepeatResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueRepeatResponse = QueueRepeatResponses[keyof QueueRepeatResponses];

export type QueueShuffleData = {
    body: QueueShuffleRequestWritable;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/shuffle';
};

export type QueueShuffleErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueShuffleError = QueueShuffleErrors[keyof QueueShuffleErrors];

export type QueueShuffleResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueShuffleResponse = QueueShuffleResponses[keyof QueueShuffleResponses];

export type QueueClearUpcomingData = {
    body?: never;
    path?: never;
    query?: {
        device_id?: string;
    };
    url: '/api/me/queue/upcoming';
};

export type QueueClearUpcomingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type QueueClearUpcomingError = QueueClearUpcomingErrors[keyof QueueClearUpcomingErrors];

export type QueueClearUpcomingResponses = {
    /**
     * No Content
     */
    204: void;
};

export type QueueClearUpcomingResponse = QueueClearUpcomingResponses[keyof QueueClearUpcomingResponses];

export type ListRadioFavoritesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/radio/favorites';
};

export type ListRadioFavoritesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListRadioFavoritesError = ListRadioFavoritesErrors[keyof ListRadioFavoritesErrors];

export type ListRadioFavoritesResponses = {
    /**
     * OK
     */
    200: RadioFavoritesBody;
};

export type ListRadioFavoritesResponse = ListRadioFavoritesResponses[keyof ListRadioFavoritesResponses];

export type AddRadioFavoriteData = {
    body: StationInputWritable;
    path?: never;
    query?: never;
    url: '/api/me/radio/favorites';
};

export type AddRadioFavoriteErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AddRadioFavoriteError = AddRadioFavoriteErrors[keyof AddRadioFavoriteErrors];

export type AddRadioFavoriteResponses = {
    /**
     * OK
     */
    200: UserRadioFavorite;
};

export type AddRadioFavoriteResponse = AddRadioFavoriteResponses[keyof AddRadioFavoriteResponses];

export type RemoveRadioFavoriteData = {
    body?: never;
    path: {
        uuid: string;
    };
    query?: never;
    url: '/api/me/radio/favorites/{uuid}';
};

export type RemoveRadioFavoriteErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RemoveRadioFavoriteError = RemoveRadioFavoriteErrors[keyof RemoveRadioFavoriteErrors];

export type RemoveRadioFavoriteResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type RemoveRadioFavoriteResponse = RemoveRadioFavoriteResponses[keyof RemoveRadioFavoriteResponses];

export type RecordRadioPlayData = {
    body: StationInputWritable;
    path?: never;
    query?: never;
    url: '/api/me/radio/play';
};

export type RecordRadioPlayErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecordRadioPlayError = RecordRadioPlayErrors[keyof RecordRadioPlayErrors];

export type RecordRadioPlayResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type RecordRadioPlayResponse = RecordRadioPlayResponses[keyof RecordRadioPlayResponses];

export type ListRadioRecentsData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/me/radio/recents';
};

export type ListRadioRecentsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListRadioRecentsError = ListRadioRecentsErrors[keyof ListRadioRecentsErrors];

export type ListRadioRecentsResponses = {
    /**
     * OK
     */
    200: RadioRecentsBody;
};

export type ListRadioRecentsResponse = ListRadioRecentsResponses[keyof ListRadioRecentsResponses];

export type ListRatedAlbumsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Filter to ratings at or above N (1..10)
         */
        min_rating?: number;
        /**
         * Filter to ratings at or below N — [min,max] bands back the Favorites reaction tabs
         */
        max_rating?: number;
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/me/ratings/albums';
};

export type ListRatedAlbumsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListRatedAlbumsError = ListRatedAlbumsErrors[keyof ListRatedAlbumsErrors];

export type ListRatedAlbumsResponses = {
    /**
     * OK
     */
    200: MusicListPageListUserRatedAlbumsRow;
};

export type ListRatedAlbumsResponse = ListRatedAlbumsResponses[keyof ListRatedAlbumsResponses];

export type BatchAlbumRatingsData = {
    body: AlbumIdsBodyWritable;
    path?: never;
    query?: never;
    url: '/api/me/ratings/albums/batch';
};

export type BatchAlbumRatingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BatchAlbumRatingsError = BatchAlbumRatingsErrors[keyof BatchAlbumRatingsErrors];

export type BatchAlbumRatingsResponses = {
    /**
     * OK
     */
    200: BatchRatingsBody;
};

export type BatchAlbumRatingsResponse = BatchAlbumRatingsResponses[keyof BatchAlbumRatingsResponses];

export type GetAlbumRatingData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/ratings/albums/{id}';
};

export type GetAlbumRatingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetAlbumRatingError = GetAlbumRatingErrors[keyof GetAlbumRatingErrors];

export type GetAlbumRatingResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type GetAlbumRatingResponse = GetAlbumRatingResponses[keyof GetAlbumRatingResponses];

export type SetAlbumRatingData = {
    body: RatingBodyWritable;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/ratings/albums/{id}';
};

export type SetAlbumRatingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetAlbumRatingError = SetAlbumRatingErrors[keyof SetAlbumRatingErrors];

export type SetAlbumRatingResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type SetAlbumRatingResponse = SetAlbumRatingResponses[keyof SetAlbumRatingResponses];

export type ListRatedArtistsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Filter to ratings at or above N (1..10)
         */
        min_rating?: number;
        /**
         * Filter to ratings at or below N — [min,max] bands back the Favorites reaction tabs
         */
        max_rating?: number;
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/me/ratings/artists';
};

export type ListRatedArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListRatedArtistsError = ListRatedArtistsErrors[keyof ListRatedArtistsErrors];

export type ListRatedArtistsResponses = {
    /**
     * OK
     */
    200: MusicListPageListUserRatedArtistsRow;
};

export type ListRatedArtistsResponse = ListRatedArtistsResponses[keyof ListRatedArtistsResponses];

export type BatchArtistRatingsData = {
    body: ArtistIdsBodyWritable;
    path?: never;
    query?: never;
    url: '/api/me/ratings/artists/batch';
};

export type BatchArtistRatingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BatchArtistRatingsError = BatchArtistRatingsErrors[keyof BatchArtistRatingsErrors];

export type BatchArtistRatingsResponses = {
    /**
     * OK
     */
    200: BatchRatingsBody;
};

export type BatchArtistRatingsResponse = BatchArtistRatingsResponses[keyof BatchArtistRatingsResponses];

export type GetArtistRatingData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/ratings/artists/{id}';
};

export type GetArtistRatingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetArtistRatingError = GetArtistRatingErrors[keyof GetArtistRatingErrors];

export type GetArtistRatingResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type GetArtistRatingResponse = GetArtistRatingResponses[keyof GetArtistRatingResponses];

export type SetArtistRatingData = {
    body: RatingBodyWritable;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/ratings/artists/{id}';
};

export type SetArtistRatingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetArtistRatingError = SetArtistRatingErrors[keyof SetArtistRatingErrors];

export type SetArtistRatingResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type SetArtistRatingResponse = SetArtistRatingResponses[keyof SetArtistRatingResponses];

export type GetFavoritesThresholdData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/ratings/threshold';
};

export type GetFavoritesThresholdErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetFavoritesThresholdError = GetFavoritesThresholdErrors[keyof GetFavoritesThresholdErrors];

export type GetFavoritesThresholdResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type GetFavoritesThresholdResponse = GetFavoritesThresholdResponses[keyof GetFavoritesThresholdResponses];

export type SetFavoritesThresholdData = {
    body: RatingBodyWritable;
    path?: never;
    query?: never;
    url: '/api/me/ratings/threshold';
};

export type SetFavoritesThresholdErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetFavoritesThresholdError = SetFavoritesThresholdErrors[keyof SetFavoritesThresholdErrors];

export type SetFavoritesThresholdResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type SetFavoritesThresholdResponse = SetFavoritesThresholdResponses[keyof SetFavoritesThresholdResponses];

export type RatedTrackStatsData = {
    body?: never;
    path?: never;
    query?: {
        min_rating?: number;
        max_rating?: number;
    };
    url: '/api/me/ratings/track-stats';
};

export type RatedTrackStatsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RatedTrackStatsError = RatedTrackStatsErrors[keyof RatedTrackStatsErrors];

export type RatedTrackStatsResponses = {
    /**
     * OK
     */
    200: GetUserRatedTracksStatsRow;
};

export type RatedTrackStatsResponse = RatedTrackStatsResponses[keyof RatedTrackStatsResponses];

export type ListRatedTracksData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Filter to ratings at or above N (1..10)
         */
        min_rating?: number;
        /**
         * Filter to ratings at or below N — [min,max] bands back the Favorites reaction tabs
         */
        max_rating?: number;
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/me/ratings/tracks';
};

export type ListRatedTracksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListRatedTracksError = ListRatedTracksErrors[keyof ListRatedTracksErrors];

export type ListRatedTracksResponses = {
    /**
     * OK
     */
    200: MusicListPageListUserRatedTracksRow;
};

export type ListRatedTracksResponse = ListRatedTracksResponses[keyof ListRatedTracksResponses];

export type BatchTrackRatingsData = {
    body: TrackIdsBodyWritable;
    path?: never;
    query?: never;
    url: '/api/me/ratings/tracks/batch';
};

export type BatchTrackRatingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BatchTrackRatingsError = BatchTrackRatingsErrors[keyof BatchTrackRatingsErrors];

export type BatchTrackRatingsResponses = {
    /**
     * OK
     */
    200: BatchRatingsBody;
};

export type BatchTrackRatingsResponse = BatchTrackRatingsResponses[keyof BatchTrackRatingsResponses];

export type GetTrackRatingData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/ratings/tracks/{id}';
};

export type GetTrackRatingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetTrackRatingError = GetTrackRatingErrors[keyof GetTrackRatingErrors];

export type GetTrackRatingResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type GetTrackRatingResponse = GetTrackRatingResponses[keyof GetTrackRatingResponses];

export type SetTrackRatingData = {
    body: RatingBodyWritable;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/me/ratings/tracks/{id}';
};

export type SetTrackRatingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetTrackRatingError = SetTrackRatingErrors[keyof SetTrackRatingErrors];

export type SetTrackRatingResponses = {
    /**
     * OK
     */
    200: RatingBody;
};

export type SetTrackRatingResponse = SetTrackRatingResponses[keyof SetTrackRatingResponses];

export type ListRecentlyPlayedData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/me/recently-played';
};

export type ListRecentlyPlayedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListRecentlyPlayedError = ListRecentlyPlayedErrors[keyof ListRecentlyPlayedErrors];

export type ListRecentlyPlayedResponses = {
    /**
     * OK
     */
    200: RecentlyPlayedBody;
};

export type ListRecentlyPlayedResponse = ListRecentlyPlayedResponses[keyof ListRecentlyPlayedResponses];

export type ForYouRecommendationsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Restrict to one media type
         */
        type?: 'movie' | 'tv' | 'anime';
        /**
         * Only titles in this genre
         */
        genre?: string;
        /**
         * Only titles carrying this keyword/tag
         */
        keyword?: string;
        /**
         * Minimum external rating
         */
        min_rating?: number;
        /**
         * Number of results
         */
        limit?: number;
        /**
         * Rank offset for paging (the engine re-ranks at most its top 200)
         */
        offset?: number;
    };
    url: '/api/me/recommendations';
};

export type ForYouRecommendationsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ForYouRecommendationsError = ForYouRecommendationsErrors[keyof ForYouRecommendationsErrors];

export type ForYouRecommendationsResponses = {
    /**
     * OK
     */
    200: ForYouResult;
};

export type ForYouRecommendationsResponse = ForYouRecommendationsResponses[keyof ForYouRecommendationsResponses];

export type RecommendedRailsData = {
    body?: never;
    path: {
        /**
         * Section to build rails for
         */
        section: 'movie' | 'tv';
    };
    query?: never;
    url: '/api/me/recommended/{section}';
};

export type RecommendedRailsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecommendedRailsError = RecommendedRailsErrors[keyof RecommendedRailsErrors];

export type RecommendedRailsResponses = {
    /**
     * OK
     */
    200: RecommendedResult;
};

export type RecommendedRailsResponse = RecommendedRailsResponses[keyof RecommendedRailsResponses];

export type RecommendedRailPageData = {
    body?: never;
    path: {
        section: 'movie' | 'tv';
    };
    query?: {
        /**
         * RecRail.key of the rail being paged
         */
        key?: 'recently-released' | 'top-unwatched' | 'by-actor' | 'more-genre' | 'recommended' | 'top-rated' | 'rediscover';
        /**
         * RecRail.baseline (genre name) where the rail has one
         */
        baseline?: string;
        /**
         * RecRail.baseline_id (person id) where the rail has one
         */
        baseline_id?: number;
        limit?: number;
        offset?: number;
    };
    url: '/api/me/recommended/{section}/rail';
};

export type RecommendedRailPageErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecommendedRailPageError = RecommendedRailPageErrors[keyof RecommendedRailPageErrors];

export type RecommendedRailPageResponses = {
    /**
     * OK
     */
    200: RailPageBody;
};

export type RecommendedRailPageResponse = RecommendedRailPageResponses[keyof RecommendedRailPageResponses];

export type SessionHeartbeatData = {
    body: SessionHeartbeatInputWritable;
    path?: never;
    query?: never;
    url: '/api/me/sessions/heartbeat';
};

export type SessionHeartbeatErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SessionHeartbeatError = SessionHeartbeatErrors[keyof SessionHeartbeatErrors];

export type SessionHeartbeatResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type SessionHeartbeatResponse = SessionHeartbeatResponses[keyof SessionHeartbeatResponses];

export type EndSessionData = {
    body?: never;
    path: {
        session_id: string;
    };
    query?: never;
    url: '/api/me/sessions/{session_id}';
};

export type EndSessionErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type EndSessionError = EndSessionErrors[keyof EndSessionErrors];

export type EndSessionResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type EndSessionResponse = EndSessionResponses[keyof EndSessionResponses];

export type GetUserSettingsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/settings';
};

export type GetUserSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetUserSettingsError = GetUserSettingsErrors[keyof GetUserSettingsErrors];

export type GetUserSettingsResponses = {
    /**
     * OK
     */
    200: UserSettings;
};

export type GetUserSettingsResponse = GetUserSettingsResponses[keyof GetUserSettingsResponses];

export type UpdateUserSettingsData = {
    body: UserSettingsWritable;
    path?: never;
    query?: never;
    url: '/api/me/settings';
};

export type UpdateUserSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateUserSettingsError = UpdateUserSettingsErrors[keyof UpdateUserSettingsErrors];

export type UpdateUserSettingsResponses = {
    /**
     * OK
     */
    200: UserSettings;
};

export type UpdateUserSettingsResponse = UpdateUserSettingsResponses[keyof UpdateUserSettingsResponses];

export type GetUserStateData = {
    body: GetUserStateRequestWritable;
    path?: never;
    query?: never;
    url: '/api/me/state';
};

export type GetUserStateErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetUserStateError = GetUserStateErrors[keyof GetUserStateErrors];

export type GetUserStateResponses = {
    /**
     * OK
     */
    200: {
        [key: string]: unknown;
    };
};

export type GetUserStateResponse = GetUserStateResponses[keyof GetUserStateResponses];

export type RevokeSubsonicCredentialData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/subsonic-credential';
};

export type RevokeSubsonicCredentialErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RevokeSubsonicCredentialError = RevokeSubsonicCredentialErrors[keyof RevokeSubsonicCredentialErrors];

export type RevokeSubsonicCredentialResponses = {
    /**
     * No Content
     */
    204: void;
};

export type RevokeSubsonicCredentialResponse = RevokeSubsonicCredentialResponses[keyof RevokeSubsonicCredentialResponses];

export type GetSubsonicCredentialData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/subsonic-credential';
};

export type GetSubsonicCredentialErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetSubsonicCredentialError = GetSubsonicCredentialErrors[keyof GetSubsonicCredentialErrors];

export type GetSubsonicCredentialResponses = {
    /**
     * OK
     */
    200: SubsonicCredentialBody;
};

export type GetSubsonicCredentialResponse = GetSubsonicCredentialResponses[keyof GetSubsonicCredentialResponses];

export type RotateSubsonicCredentialData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/subsonic-credential';
};

export type RotateSubsonicCredentialErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RotateSubsonicCredentialError = RotateSubsonicCredentialErrors[keyof RotateSubsonicCredentialErrors];

export type RotateSubsonicCredentialResponses = {
    /**
     * OK
     */
    200: SubsonicCredentialBody;
};

export type RotateSubsonicCredentialResponse = RotateSubsonicCredentialResponses[keyof RotateSubsonicCredentialResponses];

export type UpNextRailData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/me/up-next';
};

export type UpNextRailErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpNextRailError = UpNextRailErrors[keyof UpNextRailErrors];

export type UpNextRailResponses = {
    /**
     * OK
     */
    200: Array<UpNextRailItem> | null;
};

export type UpNextRailResponse = UpNextRailResponses[keyof UpNextRailResponses];

export type ContinueWatchingData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/me/watch/continue';
};

export type ContinueWatchingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ContinueWatchingError = ContinueWatchingErrors[keyof ContinueWatchingErrors];

export type ContinueWatchingResponses = {
    /**
     * OK
     */
    200: Array<ContinueWatchingEnrichedRow> | null;
};

export type ContinueWatchingResponse = ContinueWatchingResponses[keyof ContinueWatchingResponses];

export type RecentlyWatchedData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
        offset?: number;
    };
    url: '/api/me/watch/recent';
};

export type RecentlyWatchedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecentlyWatchedError = RecentlyWatchedErrors[keyof RecentlyWatchedErrors];

export type RecentlyWatchedResponses = {
    /**
     * OK
     */
    200: Array<ListRecentlyWatchedRow> | null;
};

export type RecentlyWatchedResponse = RecentlyWatchedResponses[keyof RecentlyWatchedResponses];

export type RecentlyWatchedEpisodesData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
        offset?: number;
    };
    url: '/api/me/watch/recent-episodes';
};

export type RecentlyWatchedEpisodesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecentlyWatchedEpisodesError = RecentlyWatchedEpisodesErrors[keyof RecentlyWatchedEpisodesErrors];

export type RecentlyWatchedEpisodesResponses = {
    /**
     * OK
     */
    200: Array<ListRecentlyWatchedEpisodesRow> | null;
};

export type RecentlyWatchedEpisodesResponse = RecentlyWatchedEpisodesResponses[keyof RecentlyWatchedEpisodesResponses];

export type UnmarkEpisodeWatchedData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/watched/episode/{id}';
};

export type UnmarkEpisodeWatchedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UnmarkEpisodeWatchedError = UnmarkEpisodeWatchedErrors[keyof UnmarkEpisodeWatchedErrors];

export type UnmarkEpisodeWatchedResponses = {
    /**
     * OK
     */
    200: WatchedBody;
};

export type UnmarkEpisodeWatchedResponse = UnmarkEpisodeWatchedResponses[keyof UnmarkEpisodeWatchedResponses];

export type MarkEpisodeWatchedData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/watched/episode/{id}';
};

export type MarkEpisodeWatchedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MarkEpisodeWatchedError = MarkEpisodeWatchedErrors[keyof MarkEpisodeWatchedErrors];

export type MarkEpisodeWatchedResponses = {
    /**
     * OK
     */
    200: WatchedBody;
};

export type MarkEpisodeWatchedResponse = MarkEpisodeWatchedResponses[keyof MarkEpisodeWatchedResponses];

export type MarkMediaWatchedData = {
    body: MarkMediaWatchedRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/watched/media/{id}';
};

export type MarkMediaWatchedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MarkMediaWatchedError = MarkMediaWatchedErrors[keyof MarkMediaWatchedErrors];

export type MarkMediaWatchedResponses = {
    /**
     * OK
     */
    200: WatchedBody;
};

export type MarkMediaWatchedResponse = MarkMediaWatchedResponses[keyof MarkMediaWatchedResponses];

export type MarkSeasonWatchedData = {
    body: MarkSeasonWatchedRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/me/watched/season/{id}';
};

export type MarkSeasonWatchedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MarkSeasonWatchedError = MarkSeasonWatchedErrors[keyof MarkSeasonWatchedErrors];

export type MarkSeasonWatchedResponses = {
    /**
     * OK
     */
    200: WatchedBody;
};

export type MarkSeasonWatchedResponse = MarkSeasonWatchedResponses[keyof MarkSeasonWatchedResponses];

export type ListMediaData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Media type bucket
         */
        type?: 'movie' | 'tv' | 'music' | 'book' | 'comic' | 'podcast' | 'radio';
        /**
         * title = alphabetical, added = newest first
         */
        sort?: 'title' | 'added';
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/media';
};

export type ListMediaErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListMediaError = ListMediaErrors[keyof ListMediaErrors];

export type ListMediaResponses = {
    /**
     * OK
     */
    200: Array<MediaItemView> | null;
};

export type ListMediaResponse = ListMediaResponses[keyof ListMediaResponses];

export type AmbientBackdropsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Comma-separated media types (movie,tv,anime,music,book). Empty = all five.
         */
        types?: string;
        limit?: number;
    };
    url: '/api/media/ambient-backdrops';
};

export type AmbientBackdropsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AmbientBackdropsError = AmbientBackdropsErrors[keyof AmbientBackdropsErrors];

export type AmbientBackdropsResponses = {
    /**
     * OK
     */
    200: Array<AmbientBackdropItem> | null;
};

export type AmbientBackdropsResponse = AmbientBackdropsResponses[keyof AmbientBackdropsResponses];

export type ListEnrichedMediaData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * movie or tv
         */
        type?: 'movie' | 'tv';
        limit?: number;
        offset?: number;
    };
    url: '/api/media/enriched';
};

export type ListEnrichedMediaErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListEnrichedMediaError = ListEnrichedMediaErrors[keyof ListEnrichedMediaErrors];

export type ListEnrichedMediaResponses = {
    /**
     * OK
     */
    200: EnrichedMediaBody;
};

export type ListEnrichedMediaResponse = ListEnrichedMediaResponses[keyof ListEnrichedMediaResponses];

export type CleanupMissingData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/media/missing';
};

export type CleanupMissingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CleanupMissingError = CleanupMissingErrors[keyof CleanupMissingErrors];

export type CleanupMissingResponses = {
    /**
     * OK
     */
    200: DeletedCountBody;
};

export type CleanupMissingResponse = CleanupMissingResponses[keyof CleanupMissingResponses];

export type ListMissingData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/media/missing';
};

export type ListMissingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListMissingError = ListMissingErrors[keyof ListMissingErrors];

export type ListMissingResponses = {
    /**
     * OK
     */
    200: Array<MissingMediaItem> | null;
};

export type ListMissingResponse = ListMissingResponses[keyof ListMissingResponses];

export type RecentlyAddedTvData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
        /**
         * Entry offset — deeper pages regroup the full arrival history
         */
        offset?: number;
    };
    url: '/api/media/tv/recently-added';
};

export type RecentlyAddedTvErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RecentlyAddedTvError = RecentlyAddedTvErrors[keyof RecentlyAddedTvErrors];

export type RecentlyAddedTvResponses = {
    /**
     * OK
     */
    200: Array<RecentlyAddedTvEntry> | null;
};

export type RecentlyAddedTvResponse = RecentlyAddedTvResponses[keyof RecentlyAddedTvResponses];

export type GetMediaData = {
    body?: never;
    path: {
        /**
         * Numeric ID or slug
         */
        id: string;
    };
    query?: never;
    url: '/api/media/{id}';
};

export type GetMediaErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetMediaError = GetMediaErrors[keyof GetMediaErrors];

export type GetMediaResponses = {
    /**
     * OK
     */
    200: {
        [key: string]: unknown;
    };
};

export type GetMediaResponse = GetMediaResponses[keyof GetMediaResponses];

export type DownloadAssetData = {
    body: DownloadAssetRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/assets/download';
};

export type DownloadAssetErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type DownloadAssetError = DownloadAssetErrors[keyof DownloadAssetErrors];

export type DownloadAssetResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type DownloadAssetResponse = DownloadAssetResponses[keyof DownloadAssetResponses];

export type SearchProviderArtworkData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Filter by asset type (empty = all)
         */
        type?: '' | 'poster' | 'backdrop' | 'logo' | 'art' | 'clearart' | 'banner' | 'thumb' | 'disc' | 'still';
        /**
         * Filter by provider name
         */
        provider?: string;
    };
    url: '/api/media/{id}/assets/search';
};

export type SearchProviderArtworkErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SearchProviderArtworkError = SearchProviderArtworkErrors[keyof SearchProviderArtworkErrors];

export type SearchProviderArtworkResponses = {
    /**
     * OK
     */
    200: ArtworkBody;
};

export type SearchProviderArtworkResponse = SearchProviderArtworkResponses[keyof SearchProviderArtworkResponses];

export type UploadMediaAssetData = {
    body?: {
        /**
         * Artwork slot (defaults to poster)
         */
        asset_type: 'poster' | 'backdrop' | 'logo' | 'art' | 'banner' | 'thumb' | 'disc' | 'clearart' | 'still';
        file: Blob | File;
        /**
         * Optional season/episode asset label
         */
        label: string;
    };
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/assets/upload';
};

export type UploadMediaAssetErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UploadMediaAssetError = UploadMediaAssetErrors[keyof UploadMediaAssetErrors];

export type UploadMediaAssetResponses = {
    /**
     * OK
     */
    200: UploadAssetResultBody;
};

export type UploadMediaAssetResponse = UploadMediaAssetResponses[keyof UploadMediaAssetResponses];

export type DeleteAssetData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        asset_id: number;
    };
    query?: never;
    url: '/api/media/{id}/assets/{asset_id}';
};

export type DeleteAssetErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type DeleteAssetError = DeleteAssetErrors[keyof DeleteAssetErrors];

export type DeleteAssetResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type DeleteAssetResponse = DeleteAssetResponses[keyof DeleteAssetResponses];

export type SetPrimaryAssetData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        asset_id: number;
    };
    query?: never;
    url: '/api/media/{id}/assets/{asset_id}/primary';
};

export type SetPrimaryAssetErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetPrimaryAssetError = SetPrimaryAssetErrors[keyof SetPrimaryAssetErrors];

export type SetPrimaryAssetResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type SetPrimaryAssetResponse = SetPrimaryAssetResponses[keyof SetPrimaryAssetResponses];

export type UpdateEpisodeData = {
    body: UpdateEpisodeReqWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        episode_id: number;
    };
    query?: never;
    url: '/api/media/{id}/episode/{episode_id}';
};

export type UpdateEpisodeErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateEpisodeError = UpdateEpisodeErrors[keyof UpdateEpisodeErrors];

export type UpdateEpisodeResponses = {
    /**
     * OK
     */
    200: TvEpisode;
};

export type UpdateEpisodeResponse = UpdateEpisodeResponses[keyof UpdateEpisodeResponses];

export type MediaFilesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/files';
};

export type MediaFilesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MediaFilesError = MediaFilesErrors[keyof MediaFilesErrors];

export type MediaFilesResponses = {
    /**
     * OK
     */
    200: Array<MediaFileInfo> | null;
};

export type MediaFilesResponse = MediaFilesResponses[keyof MediaFilesResponses];

export type IdentifySearchData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Title query
         */
        q?: string;
        /**
         * Year hint (4-digit)
         */
        year?: string;
    };
    url: '/api/media/{id}/identify';
};

export type IdentifySearchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type IdentifySearchError = IdentifySearchErrors[keyof IdentifySearchErrors];

export type IdentifySearchResponses = {
    /**
     * OK
     */
    200: IdentifyBody;
};

export type IdentifySearchResponse = IdentifySearchResponses[keyof IdentifySearchResponses];

export type ApplyIdentifyData = {
    body: ApplyIdentifyRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/identify';
};

export type ApplyIdentifyErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ApplyIdentifyError = ApplyIdentifyErrors[keyof ApplyIdentifyErrors];

export type ApplyIdentifyResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type ApplyIdentifyResponse = ApplyIdentifyResponses[keyof ApplyIdentifyResponses];

export type MediaImageData = {
    body?: never;
    path: {
        id: string;
        type: string;
    };
    query?: never;
    url: '/api/media/{id}/image/{type}';
};

export type MediaImageErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MediaImageError = MediaImageErrors[keyof MediaImageErrors];

export type MediaImageResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type MediaImageResponse = MediaImageResponses[keyof MediaImageResponses];

export type GetMediaLanguagesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/languages';
};

export type GetMediaLanguagesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetMediaLanguagesError = GetMediaLanguagesErrors[keyof GetMediaLanguagesErrors];

export type GetMediaLanguagesResponses = {
    /**
     * OK
     */
    200: MediaLanguages;
};

export type GetMediaLanguagesResponse = GetMediaLanguagesResponses[keyof GetMediaLanguagesResponses];

export type UpdateMediaMetadataData = {
    body: UpdateMediaMetadataReqWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/metadata';
};

export type UpdateMediaMetadataErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateMediaMetadataError = UpdateMediaMetadataErrors[keyof UpdateMediaMetadataErrors];

export type UpdateMediaMetadataResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type UpdateMediaMetadataResponse = UpdateMediaMetadataResponses[keyof UpdateMediaMetadataResponses];

export type RefreshMediaData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/refresh';
};

export type RefreshMediaErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RefreshMediaError = RefreshMediaErrors[keyof RefreshMediaErrors];

export type RefreshMediaResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RefreshMediaResponse = RefreshMediaResponses[keyof RefreshMediaResponses];

export type UpdateSeasonData = {
    body: UpdateSeasonReqWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
        season_id: number;
    };
    query?: never;
    url: '/api/media/{id}/season/{season_id}';
};

export type UpdateSeasonErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateSeasonError = UpdateSeasonErrors[keyof UpdateSeasonErrors];

export type UpdateSeasonResponses = {
    /**
     * OK
     */
    200: TvSeason;
};

export type UpdateSeasonResponse = UpdateSeasonResponses[keyof UpdateSeasonResponses];

export type GetUpNextData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Pick a random episode with a file instead of the next unwatched one
         */
        shuffle?: boolean;
        /**
         * Episode id to avoid repeating when shuffling
         */
        exclude?: number;
    };
    url: '/api/media/{id}/up-next';
};

export type GetUpNextErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetUpNextError = GetUpNextErrors[keyof GetUpNextErrors];

export type GetUpNextResponses = {
    /**
     * OK
     */
    200: UpNextResult;
};

export type GetUpNextResponse = GetUpNextResponses[keyof GetUpNextResponses];

export type GetWatchedEpisodesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/media/{id}/watched-episodes';
};

export type GetWatchedEpisodesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetWatchedEpisodesError = GetWatchedEpisodesErrors[keyof GetWatchedEpisodesErrors];

export type GetWatchedEpisodesResponses = {
    /**
     * OK
     */
    200: Array<SeasonWatchInfo> | null;
};

export type GetWatchedEpisodesResponse = GetWatchedEpisodesResponses[keyof GetWatchedEpisodesResponses];

export type MetadataImageData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/api/metadata/images/{id}';
};

export type MetadataImageErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MetadataImageError = MetadataImageErrors[keyof MetadataImageErrors];

export type MetadataImageResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type MetadataImageResponse = MetadataImageResponses[keyof MetadataImageResponses];

export type ListMusicAlbumsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/albums';
};

export type ListMusicAlbumsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListMusicAlbumsError = ListMusicAlbumsErrors[keyof ListMusicAlbumsErrors];

export type ListMusicAlbumsResponses = {
    /**
     * OK
     */
    200: MusicListPageListMusicAlbumsRow;
};

export type ListMusicAlbumsResponse = ListMusicAlbumsResponses[keyof ListMusicAlbumsResponses];

export type UpdateAlbumMetadataData = {
    body: UpdateAlbumReqWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/music/albums/{id}';
};

export type UpdateAlbumMetadataErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateAlbumMetadataError = UpdateAlbumMetadataErrors[keyof UpdateAlbumMetadataErrors];

export type UpdateAlbumMetadataResponses = {
    /**
     * OK
     */
    200: Album;
};

export type UpdateAlbumMetadataResponse = UpdateAlbumMetadataResponses[keyof UpdateAlbumMetadataResponses];

export type AlbumIdentifySearchData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        /**
         * Title query (defaults to the album title)
         */
        q?: string;
    };
    url: '/api/music/albums/{id}/identify';
};

export type AlbumIdentifySearchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AlbumIdentifySearchError = AlbumIdentifySearchErrors[keyof AlbumIdentifySearchErrors];

export type AlbumIdentifySearchResponses = {
    /**
     * OK
     */
    200: IdentifyBody;
};

export type AlbumIdentifySearchResponse = AlbumIdentifySearchResponses[keyof AlbumIdentifySearchResponses];

export type ApplyAlbumIdentifyData = {
    body: ApplyAlbumIdentifyRequestWritable;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/music/albums/{id}/identify';
};

export type ApplyAlbumIdentifyErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ApplyAlbumIdentifyError = ApplyAlbumIdentifyErrors[keyof ApplyAlbumIdentifyErrors];

export type ApplyAlbumIdentifyResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type ApplyAlbumIdentifyResponse = ApplyAlbumIdentifyResponses[keyof ApplyAlbumIdentifyResponses];

export type ListMusicArtistsData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/artists';
};

export type ListMusicArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListMusicArtistsError = ListMusicArtistsErrors[keyof ListMusicArtistsErrors];

export type ListMusicArtistsResponses = {
    /**
     * OK
     */
    200: MusicListPageListMusicArtistsRow;
};

export type ListMusicArtistsResponse = ListMusicArtistsResponses[keyof ListMusicArtistsResponses];

export type GetMusicAlbumData = {
    body?: never;
    path: {
        artist_slug: string;
        album_slug: string;
    };
    query?: never;
    url: '/api/music/artists/{artist_slug}/albums/{album_slug}';
};

export type GetMusicAlbumErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetMusicAlbumError = GetMusicAlbumErrors[keyof GetMusicAlbumErrors];

export type GetMusicAlbumResponses = {
    /**
     * OK
     */
    200: MusicAlbumDetail;
};

export type GetMusicAlbumResponse = GetMusicAlbumResponses[keyof GetMusicAlbumResponses];

export type AlbumCoverData = {
    body?: never;
    path: {
        artist_slug: string;
        album_slug: string;
    };
    query?: never;
    url: '/api/music/artists/{artist_slug}/albums/{album_slug}/cover';
};

export type AlbumCoverErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type AlbumCoverError = AlbumCoverErrors[keyof AlbumCoverErrors];

export type AlbumCoverResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type AlbumCoverResponse = AlbumCoverResponses[keyof AlbumCoverResponses];

export type SonicSimilarAlbumsData = {
    body?: never;
    path: {
        artist_slug: string;
        album_slug: string;
    };
    query?: {
        limit?: number;
    };
    url: '/api/music/artists/{artist_slug}/albums/{album_slug}/sonic-similar';
};

export type SonicSimilarAlbumsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SonicSimilarAlbumsError = SonicSimilarAlbumsErrors[keyof SonicSimilarAlbumsErrors];

export type SonicSimilarAlbumsResponses = {
    /**
     * OK
     */
    200: AlbumResultsBody;
};

export type SonicSimilarAlbumsResponse = SonicSimilarAlbumsResponses[keyof SonicSimilarAlbumsResponses];

export type GetMusicArtistData = {
    body?: never;
    path: {
        slug: string;
    };
    query?: never;
    url: '/api/music/artists/{slug}';
};

export type GetMusicArtistErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetMusicArtistError = GetMusicArtistErrors[keyof GetMusicArtistErrors];

export type GetMusicArtistResponses = {
    /**
     * OK
     */
    200: GetMusicArtistBySlugRow;
};

export type GetMusicArtistResponse = GetMusicArtistResponses[keyof GetMusicArtistResponses];

export type ListArtistAlbumsData = {
    body?: never;
    path: {
        slug: string;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/artists/{slug}/albums';
};

export type ListArtistAlbumsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListArtistAlbumsError = ListArtistAlbumsErrors[keyof ListArtistAlbumsErrors];

export type ListArtistAlbumsResponses = {
    /**
     * OK
     */
    200: MusicListPageListAlbumsByArtistSlugRow;
};

export type ListArtistAlbumsResponse = ListArtistAlbumsResponses[keyof ListArtistAlbumsResponses];

export type ArtistPlayQueueData = {
    body?: never;
    path: {
        slug: string;
    };
    query?: {
        limit?: number;
    };
    url: '/api/music/artists/{slug}/play-queue';
};

export type ArtistPlayQueueErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ArtistPlayQueueError = ArtistPlayQueueErrors[keyof ArtistPlayQueueErrors];

export type ArtistPlayQueueResponses = {
    /**
     * OK
     */
    200: ArtistPlayQueueBody;
};

export type ArtistPlayQueueResponse = ArtistPlayQueueResponses[keyof ArtistPlayQueueResponses];

export type SimilarArtistsData = {
    body?: never;
    path: {
        slug: string;
    };
    query?: never;
    url: '/api/music/artists/{slug}/similar';
};

export type SimilarArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SimilarArtistsError = SimilarArtistsErrors[keyof SimilarArtistsErrors];

export type SimilarArtistsResponses = {
    /**
     * OK
     */
    200: Array<SimilarArtistRow> | null;
};

export type SimilarArtistsResponse = SimilarArtistsResponses[keyof SimilarArtistsResponses];

export type SonicSimilarArtistsData = {
    body?: never;
    path: {
        slug: string;
    };
    query?: {
        limit?: number;
    };
    url: '/api/music/artists/{slug}/sonic-similar';
};

export type SonicSimilarArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SonicSimilarArtistsError = SonicSimilarArtistsErrors[keyof SonicSimilarArtistsErrors];

export type SonicSimilarArtistsResponses = {
    /**
     * OK
     */
    200: ArtistResultsBody;
};

export type SonicSimilarArtistsResponse = SonicSimilarArtistsResponses[keyof SonicSimilarArtistsResponses];

export type ArtistTopTracksData = {
    body?: never;
    path: {
        slug: string;
    };
    query?: {
        limit?: number;
    };
    url: '/api/music/artists/{slug}/top-tracks';
};

export type ArtistTopTracksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ArtistTopTracksError = ArtistTopTracksErrors[keyof ArtistTopTracksErrors];

export type ArtistTopTracksResponses = {
    /**
     * OK
     */
    200: TopTracksBody;
};

export type ArtistTopTracksResponse = ArtistTopTracksResponses[keyof ArtistTopTracksResponses];

export type ListArtistTracksData = {
    body?: never;
    path: {
        slug: string;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/artists/{slug}/tracks';
};

export type ListArtistTracksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListArtistTracksError = ListArtistTracksErrors[keyof ListArtistTracksErrors];

export type ListArtistTracksResponses = {
    /**
     * OK
     */
    200: MusicListPageListTracksByArtistSlugRow;
};

export type ListArtistTracksResponse = ListArtistTracksResponses[keyof ListArtistTracksResponses];

export type BrowseMusicGenresData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/music/browse/genres';
};

export type BrowseMusicGenresErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BrowseMusicGenresError = BrowseMusicGenresErrors[keyof BrowseMusicGenresErrors];

export type BrowseMusicGenresResponses = {
    /**
     * OK
     */
    200: GenreBucketsBody;
};

export type BrowseMusicGenresResponse = BrowseMusicGenresResponses[keyof BrowseMusicGenresResponses];

export type ListTracksByGenreData = {
    body?: never;
    path: {
        name: string;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/browse/genres/{name}/tracks';
};

export type ListTracksByGenreErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListTracksByGenreError = ListTracksByGenreErrors[keyof ListTracksByGenreErrors];

export type ListTracksByGenreResponses = {
    /**
     * OK
     */
    200: MusicListPageListTracksByGenreRow;
};

export type ListTracksByGenreResponse = ListTracksByGenreResponses[keyof ListTracksByGenreResponses];

export type BrowseMusicMoodsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/music/browse/moods';
};

export type BrowseMusicMoodsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BrowseMusicMoodsError = BrowseMusicMoodsErrors[keyof BrowseMusicMoodsErrors];

export type BrowseMusicMoodsResponses = {
    /**
     * OK
     */
    200: MoodBucketsBody;
};

export type BrowseMusicMoodsResponse = BrowseMusicMoodsResponses[keyof BrowseMusicMoodsResponses];

export type ListTracksByMoodData = {
    body?: never;
    path: {
        mood: string;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/browse/moods/{mood}/tracks';
};

export type ListTracksByMoodErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListTracksByMoodError = ListTracksByMoodErrors[keyof ListTracksByMoodErrors];

export type ListTracksByMoodResponses = {
    /**
     * OK
     */
    200: MusicListPageListTracksByMoodRow;
};

export type ListTracksByMoodResponse = ListTracksByMoodResponses[keyof ListTracksByMoodResponses];

export type BrowseMusicTempoData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/music/browse/tempo';
};

export type BrowseMusicTempoErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BrowseMusicTempoError = BrowseMusicTempoErrors[keyof BrowseMusicTempoErrors];

export type BrowseMusicTempoResponses = {
    /**
     * OK
     */
    200: TempoBucketsBody;
};

export type BrowseMusicTempoResponse = BrowseMusicTempoResponses[keyof BrowseMusicTempoResponses];

export type ListTracksByTempoData = {
    body?: never;
    path: {
        band: string;
    };
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/browse/tempo/{band}/tracks';
};

export type ListTracksByTempoErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListTracksByTempoError = ListTracksByTempoErrors[keyof ListTracksByTempoErrors];

export type ListTracksByTempoResponses = {
    /**
     * OK
     */
    200: MusicListPageListTracksByTempoBandRow;
};

export type ListTracksByTempoResponse = ListTracksByTempoResponses[keyof ListTracksByTempoResponses];

export type MusicCountsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/music/counts';
};

export type MusicCountsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicCountsError = MusicCountsErrors[keyof MusicCountsErrors];

export type MusicCountsResponses = {
    /**
     * OK
     */
    200: MusicCounts;
};

export type MusicCountsResponse = MusicCountsResponses[keyof MusicCountsResponses];

export type MusicHomeData2 = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/home';
};

export type MusicHomeErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeError = MusicHomeErrors[keyof MusicHomeErrors];

export type MusicHomeResponses = {
    /**
     * OK
     */
    200: MusicHomeData;
};

export type MusicHomeResponse = MusicHomeResponses[keyof MusicHomeResponses];

export type MusicHomeLapsedArtistsData = {
    body?: never;
    path?: never;
    query?: {
        picks?: number;
        albums_per_artist?: number;
    };
    url: '/api/music/home/lapsed-artists';
};

export type MusicHomeLapsedArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeLapsedArtistsError = MusicHomeLapsedArtistsErrors[keyof MusicHomeLapsedArtistsErrors];

export type MusicHomeLapsedArtistsResponses = {
    /**
     * OK
     */
    200: LapsedShelfBody;
};

export type MusicHomeLapsedArtistsResponse = MusicHomeLapsedArtistsResponses[keyof MusicHomeLapsedArtistsResponses];

export type MusicHomeMixesData = {
    body?: never;
    path?: never;
    query?: {
        max?: number;
        tracks_per_mix?: number;
    };
    url: '/api/music/home/mixes-for-you';
};

export type MusicHomeMixesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeMixesError = MusicHomeMixesErrors[keyof MusicHomeMixesErrors];

export type MusicHomeMixesResponses = {
    /**
     * OK
     */
    200: MixesBody;
};

export type MusicHomeMixesResponse = MusicHomeMixesResponses[keyof MusicHomeMixesResponses];

export type MusicHomeMixesRegenerateData = {
    body?: never;
    path?: never;
    query?: {
        max?: number;
        tracks_per_mix?: number;
    };
    url: '/api/music/home/mixes-for-you/regenerate';
};

export type MusicHomeMixesRegenerateErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeMixesRegenerateError = MusicHomeMixesRegenerateErrors[keyof MusicHomeMixesRegenerateErrors];

export type MusicHomeMixesRegenerateResponses = {
    /**
     * OK
     */
    200: MixesBody;
};

export type MusicHomeMixesRegenerateResponse = MusicHomeMixesRegenerateResponses[keyof MusicHomeMixesRegenerateResponses];

export type MusicHomeMoreByArtistsData = {
    body?: never;
    path?: never;
    query?: {
        picks?: number;
        albums_per_artist?: number;
    };
    url: '/api/music/home/more-by-artists';
};

export type MusicHomeMoreByArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeMoreByArtistsError = MusicHomeMoreByArtistsErrors[keyof MusicHomeMoreByArtistsErrors];

export type MusicHomeMoreByArtistsResponses = {
    /**
     * OK
     */
    200: MoreByArtistsBody;
};

export type MusicHomeMoreByArtistsResponse = MusicHomeMoreByArtistsResponses[keyof MusicHomeMoreByArtistsResponses];

export type MusicHomeMoreFromLabelData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/home/more-from-label';
};

export type MusicHomeMoreFromLabelErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeMoreFromLabelError = MusicHomeMoreFromLabelErrors[keyof MusicHomeMoreFromLabelErrors];

export type MusicHomeMoreFromLabelResponses = {
    /**
     * OK
     */
    200: MoreFromLabelBody;
};

export type MusicHomeMoreFromLabelResponse = MusicHomeMoreFromLabelResponses[keyof MusicHomeMoreFromLabelResponses];

export type MusicHomeMoreInGenreData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/home/more-in-genre';
};

export type MusicHomeMoreInGenreErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeMoreInGenreError = MusicHomeMoreInGenreErrors[keyof MusicHomeMoreInGenreErrors];

export type MusicHomeMoreInGenreResponses = {
    /**
     * OK
     */
    200: MoreInGenreBody;
};

export type MusicHomeMoreInGenreResponse = MusicHomeMoreInGenreResponses[keyof MusicHomeMoreInGenreResponses];

export type MusicHomeMostPlayedMonthData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/home/most-played-last-month';
};

export type MusicHomeMostPlayedMonthErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeMostPlayedMonthError = MusicHomeMostPlayedMonthErrors[keyof MusicHomeMostPlayedMonthErrors];

export type MusicHomeMostPlayedMonthResponses = {
    /**
     * OK
     */
    200: MostPlayedBody;
};

export type MusicHomeMostPlayedMonthResponse = MusicHomeMostPlayedMonthResponses[keyof MusicHomeMostPlayedMonthResponses];

export type MusicHomeOnThisDayData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/home/on-this-day';
};

export type MusicHomeOnThisDayErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeOnThisDayError = MusicHomeOnThisDayErrors[keyof MusicHomeOnThisDayErrors];

export type MusicHomeOnThisDayResponses = {
    /**
     * OK
     */
    200: OnThisDayBody;
};

export type MusicHomeOnThisDayResponse = MusicHomeOnThisDayResponses[keyof MusicHomeOnThisDayResponses];

export type MusicHomeRecentPlaylistsData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/home/recent-playlists';
};

export type MusicHomeRecentPlaylistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeRecentPlaylistsError = MusicHomeRecentPlaylistsErrors[keyof MusicHomeRecentPlaylistsErrors];

export type MusicHomeRecentPlaylistsResponses = {
    /**
     * OK
     */
    200: RecentPlaylistsBody;
};

export type MusicHomeRecentPlaylistsResponse = MusicHomeRecentPlaylistsResponses[keyof MusicHomeRecentPlaylistsResponses];

export type MusicHomeRecentlyAddedData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
        offset?: number;
    };
    url: '/api/music/home/recently-added';
};

export type MusicHomeRecentlyAddedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeRecentlyAddedError = MusicHomeRecentlyAddedErrors[keyof MusicHomeRecentlyAddedErrors];

export type MusicHomeRecentlyAddedResponses = {
    /**
     * OK
     */
    200: RecentAlbumsBody;
};

export type MusicHomeRecentlyAddedResponse = MusicHomeRecentlyAddedResponses[keyof MusicHomeRecentlyAddedResponses];

export type MusicHomeRecentArtistsData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/home/recently-played-artists';
};

export type MusicHomeRecentArtistsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MusicHomeRecentArtistsError = MusicHomeRecentArtistsErrors[keyof MusicHomeRecentArtistsErrors];

export type MusicHomeRecentArtistsResponses = {
    /**
     * OK
     */
    200: RecentArtistsBody;
};

export type MusicHomeRecentArtistsResponse = MusicHomeRecentArtistsResponses[keyof MusicHomeRecentArtistsResponses];

export type BuildMusicRadioData = {
    body: RadioRequestWritable;
    path?: never;
    query?: never;
    url: '/api/music/radio';
};

export type BuildMusicRadioErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type BuildMusicRadioError = BuildMusicRadioErrors[keyof BuildMusicRadioErrors];

export type BuildMusicRadioResponses = {
    /**
     * OK
     */
    200: RadioResponse;
};

export type BuildMusicRadioResponse = BuildMusicRadioResponses[keyof BuildMusicRadioResponses];

export type SearchMusicSonicData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Free-form audio vibe prompt
         */
        q?: string;
        limit?: number;
    };
    url: '/api/music/search-sonic';
};

export type SearchMusicSonicErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SearchMusicSonicError = SearchMusicSonicErrors[keyof SearchMusicSonicErrors];

export type SearchMusicSonicResponses = {
    /**
     * OK
     */
    200: TrackTextSearchBody;
};

export type SearchMusicSonicResponse = SearchMusicSonicResponses[keyof SearchMusicSonicResponses];

export type StationsDeepCutsData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/stations/deep-cuts';
};

export type StationsDeepCutsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StationsDeepCutsError = StationsDeepCutsErrors[keyof StationsDeepCutsErrors];

export type StationsDeepCutsResponses = {
    /**
     * OK
     */
    200: StationResponse;
};

export type StationsDeepCutsResponse = StationsDeepCutsResponses[keyof StationsDeepCutsResponses];

export type StationsLibraryRadioData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/music/stations/library-radio';
};

export type StationsLibraryRadioErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StationsLibraryRadioError = StationsLibraryRadioErrors[keyof StationsLibraryRadioErrors];

export type StationsLibraryRadioResponses = {
    /**
     * OK
     */
    200: StationResponse;
};

export type StationsLibraryRadioResponse = StationsLibraryRadioResponses[keyof StationsLibraryRadioResponses];

export type StationsRandomAlbumData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/music/stations/random-album';
};

export type StationsRandomAlbumErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StationsRandomAlbumError = StationsRandomAlbumErrors[keyof StationsRandomAlbumErrors];

export type StationsRandomAlbumResponses = {
    /**
     * OK
     */
    200: StationResponse;
};

export type StationsRandomAlbumResponse = StationsRandomAlbumResponses[keyof StationsRandomAlbumResponses];

export type StationsTimeTravelData = {
    body?: never;
    path?: never;
    query?: {
        min_year?: number;
        max_year?: number;
        limit?: number;
    };
    url: '/api/music/stations/time-travel';
};

export type StationsTimeTravelErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StationsTimeTravelError = StationsTimeTravelErrors[keyof StationsTimeTravelErrors];

export type StationsTimeTravelResponses = {
    /**
     * OK
     */
    200: StationResponse;
};

export type StationsTimeTravelResponse = StationsTimeTravelResponses[keyof StationsTimeTravelResponses];

export type ListMusicTracksData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/music/tracks';
};

export type ListMusicTracksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListMusicTracksError = ListMusicTracksErrors[keyof ListMusicTracksErrors];

export type ListMusicTracksResponses = {
    /**
     * OK
     */
    200: MusicListPageListMusicTracksRow;
};

export type ListMusicTracksResponse = ListMusicTracksResponses[keyof ListMusicTracksResponses];

export type GetMusicTrackData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/music/tracks/{id}';
};

export type GetMusicTrackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetMusicTrackError = GetMusicTrackErrors[keyof GetMusicTrackErrors];

export type GetMusicTrackResponses = {
    /**
     * OK
     */
    200: MusicTrackDetail;
};

export type GetMusicTrackResponse = GetMusicTrackResponses[keyof GetMusicTrackResponses];

export type GetTrackFacetsData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/music/tracks/{id}/facets';
};

export type GetTrackFacetsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetTrackFacetsError = GetTrackFacetsErrors[keyof GetTrackFacetsErrors];

export type GetTrackFacetsResponses = {
    /**
     * OK
     */
    200: FacetsView;
};

export type GetTrackFacetsResponse = GetTrackFacetsResponses[keyof GetTrackFacetsResponses];

export type StreamTrackFileData = {
    body?: never;
    path: {
        id: number;
        track_file_id: number;
    };
    query?: never;
    url: '/api/music/tracks/{id}/file/{track_file_id}';
};

export type StreamTrackFileErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamTrackFileError = StreamTrackFileErrors[keyof StreamTrackFileErrors];

export type StreamTrackFileResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamTrackFileResponse = StreamTrackFileResponses[keyof StreamTrackFileResponses];

export type ListTrackFilesData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/music/tracks/{id}/files';
};

export type ListTrackFilesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListTrackFilesError = ListTrackFilesErrors[keyof ListTrackFilesErrors];

export type ListTrackFilesResponses = {
    /**
     * OK
     */
    200: Array<TrackFile> | null;
};

export type ListTrackFilesResponse = ListTrackFilesResponses[keyof ListTrackFilesResponses];

export type GetTrackLyricsData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/music/tracks/{id}/lyrics';
};

export type GetTrackLyricsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetTrackLyricsError = GetTrackLyricsErrors[keyof GetTrackLyricsErrors];

export type GetTrackLyricsResponses = {
    /**
     * OK
     */
    200: LyricsResponse;
};

export type GetTrackLyricsResponse = GetTrackLyricsResponses[keyof GetTrackLyricsResponses];

export type MixToTracksData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        limit?: number;
    };
    url: '/api/music/tracks/{id}/mix-to';
};

export type MixToTracksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type MixToTracksError = MixToTracksErrors[keyof MixToTracksErrors];

export type MixToTracksResponses = {
    /**
     * OK
     */
    200: MixToBody;
};

export type MixToTracksResponse = MixToTracksResponses[keyof MixToTracksResponses];

export type SonicSimilarTracksData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: {
        limit?: number;
    };
    url: '/api/music/tracks/{id}/sonic-similar';
};

export type SonicSimilarTracksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SonicSimilarTracksError = SonicSimilarTracksErrors[keyof SonicSimilarTracksErrors];

export type SonicSimilarTracksResponses = {
    /**
     * OK
     */
    200: TrackResultsBody;
};

export type SonicSimilarTracksResponse = SonicSimilarTracksResponses[keyof SonicSimilarTracksResponses];

export type StreamTrackData = {
    body?: never;
    path: {
        id: number;
    };
    query?: {
        supports_flac_native?: boolean;
        supports_flac?: boolean;
        supports_alac?: boolean;
        supports_mp3?: boolean;
        supports_aac_audio?: boolean;
        supports_ogg_vorbis?: boolean;
        supports_opus_audio?: boolean;
        supports_opus?: boolean;
        supports_wav_pcm?: boolean;
        /**
         * AAC transcode tier — one of aac-320, aac-256, aac-192, aac-128. Omit for the default caps-based direct-or-256k-fallback behavior. Unrecognized values are ignored, not rejected.
         */
        quality?: string;
    };
    url: '/api/music/tracks/{id}/stream';
};

export type StreamTrackErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamTrackError = StreamTrackErrors[keyof StreamTrackErrors];

export type StreamTrackResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamTrackResponse = StreamTrackResponses[keyof StreamTrackResponses];

export type GetTrackWaveformData = {
    body?: never;
    path: {
        /**
         * Numeric ID
         */
        id: number;
    };
    query?: never;
    url: '/api/music/tracks/{id}/waveform';
};

export type GetTrackWaveformErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetTrackWaveformError = GetTrackWaveformErrors[keyof GetTrackWaveformErrors];

export type GetTrackWaveformResponses = {
    /**
     * OK
     */
    200: WaveformBody;
};

export type GetTrackWaveformResponse = GetTrackWaveformResponses[keyof GetTrackWaveformResponses];

export type OpensubtitlesDownloadData = {
    body: OpensubtitlesDownloadRequestWritable;
    path?: never;
    query?: never;
    url: '/api/opensubtitles/download';
};

export type OpensubtitlesDownloadErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type OpensubtitlesDownloadError = OpensubtitlesDownloadErrors[keyof OpensubtitlesDownloadErrors];

export type OpensubtitlesDownloadResponses = {
    /**
     * OK
     */
    200: OsDownloadBody;
};

export type OpensubtitlesDownloadResponse = OpensubtitlesDownloadResponses[keyof OpensubtitlesDownloadResponses];

export type OpensubtitlesSearchData = {
    body?: never;
    path?: never;
    query?: {
        imdb_id?: string;
        tmdb_id?: string;
        query?: string;
        /**
         * Empty = unspecified
         */
        type?: '' | 'movie' | 'episode' | 'all';
        /**
         * Comma-separated ISO codes
         */
        languages?: string;
        season?: number;
        episode?: number;
        /**
         * Inflate from a known media item
         */
        media_id?: number;
    };
    url: '/api/opensubtitles/search';
};

export type OpensubtitlesSearchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type OpensubtitlesSearchError = OpensubtitlesSearchErrors[keyof OpensubtitlesSearchErrors];

export type OpensubtitlesSearchResponses = {
    /**
     * OK
     */
    200: SearchResponse;
};

export type OpensubtitlesSearchResponse = OpensubtitlesSearchResponses[keyof OpensubtitlesSearchResponses];

export type OpensubtitlesTestData = {
    body: OsCredentialsWritable;
    path?: never;
    query?: never;
    url: '/api/opensubtitles/test';
};

export type OpensubtitlesTestErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type OpensubtitlesTestError = OpensubtitlesTestErrors[keyof OpensubtitlesTestErrors];

export type OpensubtitlesTestResponses = {
    /**
     * OK
     */
    200: OsTestBody;
};

export type OpensubtitlesTestResponse = OpensubtitlesTestResponses[keyof OpensubtitlesTestResponses];

export type OpensubtitlesUserInfoData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/opensubtitles/user-info';
};

export type OpensubtitlesUserInfoErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type OpensubtitlesUserInfoError = OpensubtitlesUserInfoErrors[keyof OpensubtitlesUserInfoErrors];

export type OpensubtitlesUserInfoResponses = {
    /**
     * OK
     */
    200: UserInfo;
};

export type OpensubtitlesUserInfoResponse = OpensubtitlesUserInfoResponses[keyof OpensubtitlesUserInfoResponses];

export type PeopleMediaIdsData = {
    body: PeopleMediaIdsRequestWritable;
    path?: never;
    query?: never;
    url: '/api/people/media-ids';
};

export type PeopleMediaIdsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PeopleMediaIdsError = PeopleMediaIdsErrors[keyof PeopleMediaIdsErrors];

export type PeopleMediaIdsResponses = {
    /**
     * OK
     */
    200: Array<number> | null;
};

export type PeopleMediaIdsResponse = PeopleMediaIdsResponses[keyof PeopleMediaIdsResponses];

export type SearchPeopleData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Query string
         */
        q?: string;
        limit?: number;
    };
    url: '/api/people/search';
};

export type SearchPeopleErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SearchPeopleError = SearchPeopleErrors[keyof SearchPeopleErrors];

export type SearchPeopleResponses = {
    /**
     * OK
     */
    200: Array<SearchPeopleByNameRow> | null;
};

export type SearchPeopleResponse = SearchPeopleResponses[keyof SearchPeopleResponses];

export type GetPersonData = {
    body?: never;
    path: {
        /**
         * Numeric ID or slug
         */
        id: string;
    };
    query?: never;
    url: '/api/person/{id}';
};

export type GetPersonErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetPersonError = GetPersonErrors[keyof GetPersonErrors];

export type GetPersonResponses = {
    /**
     * OK
     */
    200: {
        [key: string]: unknown;
    };
};

export type GetPersonResponse = GetPersonResponses[keyof GetPersonResponses];

export type PersonImageData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/person/{id}/image';
};

export type PersonImageErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PersonImageError = PersonImageErrors[keyof PersonImageErrors];

export type PersonImageResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type PersonImageResponse = PersonImageResponses[keyof PersonImageResponses];

export type CreateNativePlaybackGrantData = {
    body: CreateNativePlaybackGrantRequestWritable;
    path?: never;
    query?: never;
    url: '/api/playback/native/grants';
};

export type CreateNativePlaybackGrantErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CreateNativePlaybackGrantError = CreateNativePlaybackGrantErrors[keyof CreateNativePlaybackGrantErrors];

export type CreateNativePlaybackGrantResponses = {
    /**
     * OK
     */
    200: NativePlaybackGrantBody;
};

export type CreateNativePlaybackGrantResponse = CreateNativePlaybackGrantResponses[keyof CreateNativePlaybackGrantResponses];

export type NativePlaybackStreamVideoData = {
    body?: never;
    headers?: {
        'X-Heya-Playback-Grant'?: string;
    };
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/playback/native/media/{file_id}';
};

export type NativePlaybackStreamVideoErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type NativePlaybackStreamVideoError = NativePlaybackStreamVideoErrors[keyof NativePlaybackStreamVideoErrors];

export type NativePlaybackStreamVideoResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type NativePlaybackStreamVideoResponse = NativePlaybackStreamVideoResponses[keyof NativePlaybackStreamVideoResponses];

export type NativePlaybackStreamHlsIndexData = {
    body?: never;
    headers?: {
        'X-Heya-Playback-Grant'?: string;
    };
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/playback/native/media/{file_id}/hls/index.m3u8';
};

export type NativePlaybackStreamHlsIndexErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type NativePlaybackStreamHlsIndexError = NativePlaybackStreamHlsIndexErrors[keyof NativePlaybackStreamHlsIndexErrors];

export type NativePlaybackStreamHlsIndexResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type NativePlaybackStreamHlsIndexResponse = NativePlaybackStreamHlsIndexResponses[keyof NativePlaybackStreamHlsIndexResponses];

export type NativePlaybackStreamHlsMasterData = {
    body?: never;
    headers?: {
        'X-Heya-Playback-Grant'?: string;
    };
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/playback/native/media/{file_id}/hls/master.m3u8';
};

export type NativePlaybackStreamHlsMasterErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type NativePlaybackStreamHlsMasterError = NativePlaybackStreamHlsMasterErrors[keyof NativePlaybackStreamHlsMasterErrors];

export type NativePlaybackStreamHlsMasterResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type NativePlaybackStreamHlsMasterResponse = NativePlaybackStreamHlsMasterResponses[keyof NativePlaybackStreamHlsMasterResponses];

export type NativePlaybackStreamHlsSegmentData = {
    body?: never;
    headers?: {
        'X-Heya-Playback-Grant'?: string;
    };
    path: {
        file_id: string;
        segment: string;
    };
    query?: never;
    url: '/api/playback/native/media/{file_id}/hls/{segment}';
};

export type NativePlaybackStreamHlsSegmentErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type NativePlaybackStreamHlsSegmentError = NativePlaybackStreamHlsSegmentErrors[keyof NativePlaybackStreamHlsSegmentErrors];

export type NativePlaybackStreamHlsSegmentResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type NativePlaybackStreamHlsSegmentResponse = NativePlaybackStreamHlsSegmentResponses[keyof NativePlaybackStreamHlsSegmentResponses];

export type NativePlaybackStreamSubtitleData = {
    body?: never;
    headers?: {
        'X-Heya-Playback-Grant'?: string;
    };
    path: {
        file_id: string;
        index: number;
    };
    query?: never;
    url: '/api/playback/native/media/{file_id}/subtitles/{index}';
};

export type NativePlaybackStreamSubtitleErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type NativePlaybackStreamSubtitleError = NativePlaybackStreamSubtitleErrors[keyof NativePlaybackStreamSubtitleErrors];

export type NativePlaybackStreamSubtitleResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type NativePlaybackStreamSubtitleResponse = NativePlaybackStreamSubtitleResponses[keyof NativePlaybackStreamSubtitleResponses];

export type PodcastsCategoriesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/podcasts/categories';
};

export type PodcastsCategoriesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PodcastsCategoriesError = PodcastsCategoriesErrors[keyof PodcastsCategoriesErrors];

export type PodcastsCategoriesResponses = {
    /**
     * OK
     */
    200: PodcastCategoriesBody;
};

export type PodcastsCategoriesResponse = PodcastsCategoriesResponses[keyof PodcastsCategoriesResponses];

export type StreamPodcastEpisodeData = {
    body?: never;
    path?: never;
    query?: {
        url?: string;
    };
    url: '/api/podcasts/episode/stream';
};

export type StreamPodcastEpisodeErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamPodcastEpisodeError = StreamPodcastEpisodeErrors[keyof StreamPodcastEpisodeErrors];

export type StreamPodcastEpisodeResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamPodcastEpisodeResponse = StreamPodcastEpisodeResponses[keyof StreamPodcastEpisodeResponses];

export type PodcastsFeedData = {
    body?: never;
    path?: never;
    query?: {
        url?: string;
    };
    url: '/api/podcasts/feed';
};

export type PodcastsFeedErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PodcastsFeedError = PodcastsFeedErrors[keyof PodcastsFeedErrors];

export type PodcastsFeedResponses = {
    /**
     * OK
     */
    200: PodcastDetail;
};

export type PodcastsFeedResponse = PodcastsFeedResponses[keyof PodcastsFeedResponses];

export type PodcastsSearchData = {
    body?: never;
    path?: never;
    query?: {
        q?: string;
        max?: number;
    };
    url: '/api/podcasts/search';
};

export type PodcastsSearchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PodcastsSearchError = PodcastsSearchErrors[keyof PodcastsSearchErrors];

export type PodcastsSearchResponses = {
    /**
     * OK
     */
    200: PodcastsBody;
};

export type PodcastsSearchResponse = PodcastsSearchResponses[keyof PodcastsSearchResponses];

export type PodcastsTrendingData = {
    body?: never;
    path?: never;
    query?: {
        max?: number;
        category?: string;
    };
    url: '/api/podcasts/trending';
};

export type PodcastsTrendingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type PodcastsTrendingError = PodcastsTrendingErrors[keyof PodcastsTrendingErrors];

export type PodcastsTrendingResponses = {
    /**
     * OK
     */
    200: PodcastsBody;
};

export type PodcastsTrendingResponse = PodcastsTrendingResponses[keyof PodcastsTrendingResponses];

export type RadioCountriesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/radio/countries';
};

export type RadioCountriesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RadioCountriesError = RadioCountriesErrors[keyof RadioCountriesErrors];

export type RadioCountriesResponses = {
    /**
     * OK
     */
    200: RadioCountriesBody;
};

export type RadioCountriesResponse = RadioCountriesResponses[keyof RadioCountriesResponses];

export type RadioSearchData = {
    body?: never;
    path?: never;
    query?: {
        name?: string;
        tag?: string;
        country?: string;
        countrycode?: string;
        limit?: number;
        offset?: number;
    };
    url: '/api/radio/search';
};

export type RadioSearchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RadioSearchError = RadioSearchErrors[keyof RadioSearchErrors];

export type RadioSearchResponses = {
    /**
     * OK
     */
    200: StationsBody;
};

export type RadioSearchResponse = RadioSearchResponses[keyof RadioSearchResponses];

export type StreamRadioData = {
    body?: never;
    path?: never;
    query?: {
        url?: string;
    };
    url: '/api/radio/stream';
};

export type StreamRadioErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamRadioError = StreamRadioErrors[keyof StreamRadioErrors];

export type StreamRadioResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamRadioResponse = StreamRadioResponses[keyof StreamRadioResponses];

export type RadioTagsData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/radio/tags';
};

export type RadioTagsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RadioTagsError = RadioTagsErrors[keyof RadioTagsErrors];

export type RadioTagsResponses = {
    /**
     * OK
     */
    200: RadioTagsBody;
};

export type RadioTagsResponse = RadioTagsResponses[keyof RadioTagsResponses];

export type RadioTopData = {
    body?: never;
    path?: never;
    query?: {
        category?: 'topvote' | 'topclick' | 'lastchange';
        count?: number;
    };
    url: '/api/radio/top';
};

export type RadioTopErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RadioTopError = RadioTopErrors[keyof RadioTopErrors];

export type RadioTopResponses = {
    /**
     * OK
     */
    200: StationsBody;
};

export type RadioTopResponse = RadioTopResponses[keyof RadioTopResponses];

export type ListRecommendationsData = {
    body?: never;
    path?: never;
    query?: {
        limit?: number;
    };
    url: '/api/recommendations';
};

export type ListRecommendationsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListRecommendationsError = ListRecommendationsErrors[keyof ListRecommendationsErrors];

export type ListRecommendationsResponses = {
    /**
     * OK
     */
    200: Array<RecItem> | null;
};

export type ListRecommendationsResponse = ListRecommendationsResponses[keyof ListRecommendationsResponses];

export type RemoteCheckData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/remote/check';
};

export type RemoteCheckErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RemoteCheckError = RemoteCheckErrors[keyof RemoteCheckErrors];

export type RemoteCheckResponses = {
    /**
     * OK
     */
    200: RemoteStatusBody;
};

export type RemoteCheckResponse = RemoteCheckResponses[keyof RemoteCheckResponses];

export type SetRemoteConfigData = {
    body: RemoteConfigPayloadWritable;
    path?: never;
    query?: never;
    url: '/api/remote/config';
};

export type SetRemoteConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetRemoteConfigError = SetRemoteConfigErrors[keyof SetRemoteConfigErrors];

export type SetRemoteConfigResponses = {
    /**
     * OK
     */
    200: StatusBody;
};

export type SetRemoteConfigResponse = SetRemoteConfigResponses[keyof SetRemoteConfigResponses];

export type RemoteStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/remote/status';
};

export type RemoteStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RemoteStatusError = RemoteStatusErrors[keyof RemoteStatusErrors];

export type RemoteStatusResponses = {
    /**
     * OK
     */
    200: RemoteStatusBody;
};

export type RemoteStatusResponse = RemoteStatusResponses[keyof RemoteStatusResponses];

export type SearchAllData = {
    body?: never;
    path?: never;
    query?: {
        q?: string;
        /**
         * Optional bucket
         */
        type?: 'movie' | 'tv' | 'music' | 'book' | 'comic' | 'podcast' | 'radio' | 'episode' | 'person' | 'albums' | 'tracks';
        /**
         * Max results
         */
        limit?: number;
        /**
         * Results offset
         */
        offset?: number;
    };
    url: '/api/search';
};

export type SearchAllErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SearchAllError = SearchAllErrors[keyof SearchAllErrors];

export type SearchAllResponses = {
    /**
     * OK
     */
    200: SearchBucket;
};

export type SearchAllResponse = SearchAllResponses[keyof SearchAllResponses];

export type SearchQuickData = {
    body?: never;
    path?: never;
    query?: {
        q?: string;
    };
    url: '/api/search/quick';
};

export type SearchQuickErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SearchQuickError = SearchQuickErrors[keyof SearchQuickErrors];

export type SearchQuickResponses = {
    /**
     * OK
     */
    200: QuickSearchResult;
};

export type SearchQuickResponse = SearchQuickResponses[keyof SearchQuickResponses];

export type SemanticSearchData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Natural-language query
         */
        q?: string;
        /**
         * Restrict to one media type
         */
        type?: 'movie' | 'tv' | 'anime';
        limit?: number;
    };
    url: '/api/search/semantic';
};

export type SemanticSearchErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SemanticSearchError = SemanticSearchErrors[keyof SemanticSearchErrors];

export type SemanticSearchResponses = {
    /**
     * OK
     */
    200: SemanticSearchResult;
};

export type SemanticSearchResponse = SemanticSearchResponses[keyof SemanticSearchResponses];

export type ListActiveSessionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/sessions/active';
};

export type ListActiveSessionsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListActiveSessionsError = ListActiveSessionsErrors[keyof ListActiveSessionsErrors];

export type ListActiveSessionsResponses = {
    /**
     * OK
     */
    200: ActiveSessionsBody;
};

export type ListActiveSessionsResponse = ListActiveSessionsResponses[keyof ListActiveSessionsResponses];

export type SessionCommandData = {
    body: SessionCommandInputWritable;
    path: {
        session_id: string;
    };
    query?: never;
    url: '/api/sessions/{session_id}/command';
};

export type SessionCommandErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SessionCommandError = SessionCommandErrors[keyof SessionCommandErrors];

export type SessionCommandResponses = {
    /**
     * OK
     */
    200: OkBody;
};

export type SessionCommandResponse = SessionCommandResponses[keyof SessionCommandResponses];

export type DashboardStatsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/stats';
};

export type DashboardStatsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type DashboardStatsError = DashboardStatsErrors[keyof DashboardStatsErrors];

export type DashboardStatsResponses = {
    /**
     * OK
     */
    200: DashboardStats;
};

export type DashboardStatsResponse = DashboardStatsResponses[keyof DashboardStatsResponses];

export type StreamDirectData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/stream/{file_id}';
};

export type StreamDirectErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamDirectError = StreamDirectErrors[keyof StreamDirectErrors];

export type StreamDirectResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamDirectResponse = StreamDirectResponses[keyof StreamDirectResponses];

export type StreamHlsIndexData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: {
        audio?: string;
    };
    url: '/api/stream/{file_id}/hls/index.m3u8';
};

export type StreamHlsIndexErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamHlsIndexError = StreamHlsIndexErrors[keyof StreamHlsIndexErrors];

export type StreamHlsIndexResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamHlsIndexResponse = StreamHlsIndexResponses[keyof StreamHlsIndexResponses];

export type StreamHlsMasterData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/stream/{file_id}/hls/master.m3u8';
};

export type StreamHlsMasterErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamHlsMasterError = StreamHlsMasterErrors[keyof StreamHlsMasterErrors];

export type StreamHlsMasterResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamHlsMasterResponse = StreamHlsMasterResponses[keyof StreamHlsMasterResponses];

export type StreamHlsSegmentData = {
    body?: never;
    path: {
        file_id: string;
        segment: string;
    };
    query?: never;
    url: '/api/stream/{file_id}/hls/{segment}';
};

export type StreamHlsSegmentErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamHlsSegmentError = StreamHlsSegmentErrors[keyof StreamHlsSegmentErrors];

export type StreamHlsSegmentResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamHlsSegmentResponse = StreamHlsSegmentResponses[keyof StreamHlsSegmentResponses];

export type StreamInfoData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: {
        supports_hevc?: boolean;
        supports_av1?: boolean;
        supports_flac?: boolean;
        supports_opus?: boolean;
        supports_ac3?: boolean;
        supports_eac3?: boolean;
        supports_mkv?: boolean;
        supports_webm?: boolean;
        supports_hdr?: boolean;
        supports_hdr10?: boolean;
        supports_hlg?: boolean;
        supports_dovi?: boolean;
        supports_hevc_hev1?: boolean;
    };
    url: '/api/stream/{file_id}/info';
};

export type StreamInfoErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamInfoError = StreamInfoErrors[keyof StreamInfoErrors];

export type StreamInfoResponses = {
    /**
     * OK
     */
    200: StreamInfoResponse;
};

export type StreamInfoResponse2 = StreamInfoResponses[keyof StreamInfoResponses];

export type StreamSegmentsData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/stream/{file_id}/segments';
};

export type StreamSegmentsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamSegmentsError = StreamSegmentsErrors[keyof StreamSegmentsErrors];

export type StreamSegmentsResponses = {
    /**
     * OK
     */
    200: FileSegmentsResponse;
};

export type StreamSegmentsResponse = StreamSegmentsResponses[keyof StreamSegmentsResponses];

export type ListSubtitlesData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/stream/{file_id}/subtitles';
};

export type ListSubtitlesErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListSubtitlesError = ListSubtitlesErrors[keyof ListSubtitlesErrors];

export type ListSubtitlesResponses = {
    /**
     * OK
     */
    200: Array<SubtitleTrack> | null;
};

export type ListSubtitlesResponse = ListSubtitlesResponses[keyof ListSubtitlesResponses];

export type StreamSubtitleBodyData = {
    body?: never;
    path: {
        file_id: string;
        index: number;
    };
    query?: never;
    url: '/api/stream/{file_id}/subtitles/{index}';
};

export type StreamSubtitleBodyErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamSubtitleBodyError = StreamSubtitleBodyErrors[keyof StreamSubtitleBodyErrors];

export type StreamSubtitleBodyResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StreamSubtitleBodyResponse = StreamSubtitleBodyResponses[keyof StreamSubtitleBodyResponses];

export type StreamTranscodeStatusData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: {
        /**
         * Zero-based audio track used by the HLS session
         */
        audio?: number;
        /**
         * Playback session id carried by the HLS manifest
         */
        sid?: string;
    };
    url: '/api/stream/{file_id}/transcode-status';
};

export type StreamTranscodeStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StreamTranscodeStatusError = StreamTranscodeStatusErrors[keyof StreamTranscodeStatusErrors];

export type StreamTranscodeStatusResponses = {
    /**
     * OK
     */
    200: TranscodeProgressResponse;
};

export type StreamTranscodeStatusResponse = StreamTranscodeStatusResponses[keyof StreamTranscodeStatusResponses];

export type TrickplayVttData = {
    body?: never;
    path: {
        file_id: string;
    };
    query?: never;
    url: '/api/stream/{file_id}/trickplay/index.vtt';
};

export type TrickplayVttErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TrickplayVttError = TrickplayVttErrors[keyof TrickplayVttErrors];

export type TrickplayVttResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type TrickplayVttResponse = TrickplayVttResponses[keyof TrickplayVttResponses];

export type TrickplaySpriteData = {
    body?: never;
    path: {
        file_id: string;
        filename: string;
    };
    query?: never;
    url: '/api/stream/{file_id}/trickplay/{filename}';
};

export type TrickplaySpriteErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TrickplaySpriteError = TrickplaySpriteErrors[keyof TrickplaySpriteErrors];

export type TrickplaySpriteResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type TrickplaySpriteResponse = TrickplaySpriteResponses[keyof TrickplaySpriteResponses];

export type StudioImageData = {
    body?: never;
    path: {
        id: number;
    };
    query?: never;
    url: '/api/studio/{id}/image';
};

export type StudioImageErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StudioImageError = StudioImageErrors[keyof StudioImageErrors];

export type StudioImageResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type StudioImageResponse = StudioImageResponses[keyof StudioImageResponses];

export type StudiosMediaIdsData = {
    body: StudiosMediaIdsRequestWritable;
    path?: never;
    query?: never;
    url: '/api/studios/media-ids';
};

export type StudiosMediaIdsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type StudiosMediaIdsError = StudiosMediaIdsErrors[keyof StudiosMediaIdsErrors];

export type StudiosMediaIdsResponses = {
    /**
     * OK
     */
    200: Array<number> | null;
};

export type StudiosMediaIdsResponse = StudiosMediaIdsResponses[keyof StudiosMediaIdsResponses];

export type SearchStudiosData = {
    body?: never;
    path?: never;
    query?: {
        q?: string;
        limit?: number;
    };
    url: '/api/studios/search';
};

export type SearchStudiosErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SearchStudiosError = SearchStudiosErrors[keyof SearchStudiosErrors];

export type SearchStudiosResponses = {
    /**
     * OK
     */
    200: Array<SearchProductionCompaniesByNameRow> | null;
};

export type SearchStudiosResponse = SearchStudiosResponses[keyof SearchStudiosResponses];

export type SubsonicConfigData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/subsonic/config';
};

export type SubsonicConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SubsonicConfigError = SubsonicConfigErrors[keyof SubsonicConfigErrors];

export type SubsonicConfigResponses = {
    /**
     * OK
     */
    200: SubsonicConfigBody;
};

export type SubsonicConfigResponse = SubsonicConfigResponses[keyof SubsonicConfigResponses];

export type SetSubsonicConfigData = {
    body: SubsonicConfigBodyWritable;
    path?: never;
    query?: never;
    url: '/api/subsonic/config';
};

export type SetSubsonicConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetSubsonicConfigError = SetSubsonicConfigErrors[keyof SetSubsonicConfigErrors];

export type SetSubsonicConfigResponses = {
    /**
     * OK
     */
    200: SubsonicConfigBody;
};

export type SetSubsonicConfigResponse = SetSubsonicConfigResponses[keyof SetSubsonicConfigResponses];

export type GetSystemSettingData = {
    body?: never;
    path: {
        /**
         * Setting key (lowercase, dots/dashes/underscores allowed)
         */
        key: string;
    };
    query?: never;
    url: '/api/system-settings/{key}';
};

export type GetSystemSettingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type GetSystemSettingError = GetSystemSettingErrors[keyof GetSystemSettingErrors];

export type GetSystemSettingResponses = {
    /**
     * OK
     */
    200: SystemSettingBody;
};

export type GetSystemSettingResponse = GetSystemSettingResponses[keyof GetSystemSettingResponses];

export type SetSystemSettingData = {
    body: SetSystemSettingRequestWritable;
    path: {
        /**
         * Setting key
         */
        key: string;
    };
    query?: never;
    url: '/api/system-settings/{key}';
};

export type SetSystemSettingErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetSystemSettingError = SetSystemSettingErrors[keyof SetSystemSettingErrors];

export type SetSystemSettingResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type SetSystemSettingResponse = SetSystemSettingResponses[keyof SetSystemSettingResponses];

export type SetTailscaleConfigData = {
    body: TailscaleConfigPayloadWritable;
    path?: never;
    query?: never;
    url: '/api/tailscale/config';
};

export type SetTailscaleConfigErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type SetTailscaleConfigError = SetTailscaleConfigErrors[keyof SetTailscaleConfigErrors];

export type SetTailscaleConfigResponses = {
    /**
     * OK
     */
    200: StatusBody;
};

export type SetTailscaleConfigResponse = SetTailscaleConfigResponses[keyof SetTailscaleConfigResponses];

export type ToggleTailscaleFunnelData = {
    body: ToggleTailscaleFunnelRequestWritable;
    path?: never;
    query?: never;
    url: '/api/tailscale/funnel';
};

export type ToggleTailscaleFunnelErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ToggleTailscaleFunnelError = ToggleTailscaleFunnelErrors[keyof ToggleTailscaleFunnelErrors];

export type ToggleTailscaleFunnelResponses = {
    /**
     * OK
     */
    200: FunnelBody;
};

export type ToggleTailscaleFunnelResponse = ToggleTailscaleFunnelResponses[keyof ToggleTailscaleFunnelResponses];

export type TailscaleLogoutData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/tailscale/logout';
};

export type TailscaleLogoutErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TailscaleLogoutError = TailscaleLogoutErrors[keyof TailscaleLogoutErrors];

export type TailscaleLogoutResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type TailscaleLogoutResponse = TailscaleLogoutResponses[keyof TailscaleLogoutResponses];

export type TailscaleRawStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/tailscale/raw';
};

export type TailscaleRawStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TailscaleRawStatusError = TailscaleRawStatusErrors[keyof TailscaleRawStatusErrors];

export type TailscaleRawStatusResponses = {
    /**
     * OK
     */
    200: unknown;
};

export type TailscaleStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/tailscale/status';
};

export type TailscaleStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TailscaleStatusError = TailscaleStatusErrors[keyof TailscaleStatusErrors];

export type TailscaleStatusResponses = {
    /**
     * OK
     */
    200: TailscaleStatusBody;
};

export type TailscaleStatusResponse = TailscaleStatusResponses[keyof TailscaleStatusResponses];

export type ListTasksData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/tasks';
};

export type ListTasksErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ListTasksError = ListTasksErrors[keyof ListTasksErrors];

export type ListTasksResponses = {
    /**
     * OK
     */
    200: Array<TaskResponse> | null;
};

export type ListTasksResponse = ListTasksResponses[keyof ListTasksResponses];

export type UpdateTaskData = {
    body: UpdateTaskRequestWritable;
    path: {
        /**
         * Task identifier
         */
        id: 'generate_trickplay' | 'generate_thumbnails' | 'scan_libraries' | 'refresh_stale_items' | 'scan_music_loudness' | 'scan_music_fingerprint' | 'scan_media_segments' | 'detect_media_segments' | 'analyze_music_facets' | 'cleanup_scanner_artifacts' | 'embed_recommendations' | 'sync_music_services';
    };
    query?: never;
    url: '/api/tasks/{id}';
};

export type UpdateTaskErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateTaskError = UpdateTaskErrors[keyof UpdateTaskErrors];

export type UpdateTaskResponses = {
    /**
     * OK
     */
    200: TaskResponse;
};

export type UpdateTaskResponse = UpdateTaskResponses[keyof UpdateTaskResponses];

export type CancelTaskData = {
    body?: never;
    path: {
        /**
         * Task identifier
         */
        id: 'generate_trickplay' | 'generate_thumbnails' | 'scan_libraries' | 'refresh_stale_items' | 'scan_music_loudness' | 'scan_music_fingerprint' | 'scan_media_segments' | 'detect_media_segments' | 'analyze_music_facets' | 'cleanup_scanner_artifacts' | 'embed_recommendations' | 'sync_music_services';
    };
    query?: never;
    url: '/api/tasks/{id}/cancel';
};

export type CancelTaskErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type CancelTaskError = CancelTaskErrors[keyof CancelTaskErrors];

export type CancelTaskResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type CancelTaskResponse = CancelTaskResponses[keyof CancelTaskResponses];

export type TaskItemsData = {
    body?: never;
    path: {
        /**
         * Task identifier
         */
        id: 'generate_trickplay' | 'generate_thumbnails' | 'scan_libraries' | 'refresh_stale_items' | 'scan_music_loudness' | 'scan_music_fingerprint' | 'scan_media_segments' | 'detect_media_segments' | 'analyze_music_facets' | 'cleanup_scanner_artifacts' | 'embed_recommendations' | 'sync_music_services';
    };
    query?: {
        /**
         * Filter by item status (pending/running/done/error)
         */
        status?: string;
        limit?: number;
        offset?: number;
    };
    url: '/api/tasks/{id}/items';
};

export type TaskItemsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TaskItemsError = TaskItemsErrors[keyof TaskItemsErrors];

export type TaskItemsResponses = {
    /**
     * OK
     */
    200: TaskItemsResult;
};

export type TaskItemsResponse = TaskItemsResponses[keyof TaskItemsResponses];

export type RunTaskData = {
    body?: never;
    path: {
        /**
         * Task identifier
         */
        id: 'generate_trickplay' | 'generate_thumbnails' | 'scan_libraries' | 'refresh_stale_items' | 'scan_music_loudness' | 'scan_music_fingerprint' | 'scan_media_segments' | 'detect_media_segments' | 'analyze_music_facets' | 'cleanup_scanner_artifacts' | 'embed_recommendations' | 'sync_music_services';
    };
    query?: never;
    url: '/api/tasks/{id}/run';
};

export type RunTaskErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type RunTaskError = RunTaskErrors[keyof RunTaskErrors];

export type RunTaskResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type RunTaskResponse = RunTaskResponses[keyof RunTaskResponses];

export type TmdbImageProxyData = {
    body?: never;
    path: {
        path: string;
    };
    query?: never;
    url: '/api/tmdb/image/{path}';
};

export type TmdbImageProxyErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TmdbImageProxyError = TmdbImageProxyErrors[keyof TmdbImageProxyErrors];

export type TmdbImageProxyResponses = {
    /**
     * Binary response — content type set per endpoint
     */
    200: Blob | File;
};

export type TmdbImageProxyResponse = TmdbImageProxyResponses[keyof TmdbImageProxyResponses];

export type ClearTranscodeCacheData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/transcode/cache';
};

export type ClearTranscodeCacheErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type ClearTranscodeCacheError = ClearTranscodeCacheErrors[keyof ClearTranscodeCacheErrors];

export type ClearTranscodeCacheResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type ClearTranscodeCacheResponse = ClearTranscodeCacheResponses[keyof ClearTranscodeCacheResponses];

export type UpdateTranscodeSettingsData = {
    body: UpdateTranscodeSettingsRequestWritable;
    path?: never;
    query?: never;
    url: '/api/transcode/settings';
};

export type UpdateTranscodeSettingsErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type UpdateTranscodeSettingsError = UpdateTranscodeSettingsErrors[keyof UpdateTranscodeSettingsErrors];

export type UpdateTranscodeSettingsResponses = {
    /**
     * OK
     */
    200: StatusOutputBody;
};

export type UpdateTranscodeSettingsResponse = UpdateTranscodeSettingsResponses[keyof UpdateTranscodeSettingsResponses];

export type TranscodeStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/transcode/status';
};

export type TranscodeStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type TranscodeStatusError = TranscodeStatusErrors[keyof TranscodeStatusErrors];

export type TranscodeStatusResponses = {
    /**
     * OK
     */
    200: TranscodeStatusBody;
};

export type TranscodeStatusResponse = TranscodeStatusResponses[keyof TranscodeStatusResponses];

export type WatcherStatusData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/api/watchers';
};

export type WatcherStatusErrors = {
    /**
     * Error
     */
    default: ErrorModel;
};

export type WatcherStatusError = WatcherStatusErrors[keyof WatcherStatusErrors];

export type WatcherStatusResponses = {
    /**
     * OK
     */
    200: WatcherStatusBody;
};

export type WatcherStatusResponse = WatcherStatusResponses[keyof WatcherStatusResponses];
