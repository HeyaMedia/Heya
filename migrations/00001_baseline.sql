-- +goose Up

-- Consolidated local baseline after the scanner-v2 schema reset.
-- This is intentionally a fresh-install schema, not a production migration.

--
-- PostgreSQL database dump
--


-- Dumped from database version 17.10 (Debian 17.10-1.pgdg12+1)
-- Dumped by pg_dump version 18.4


--
-- Name: pg_trgm; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;


--
-- Name: EXTENSION pg_trgm; Type: COMMENT; Schema: -; Owner: -
--



--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--



--
-- Name: vector; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;


--
-- Name: EXTENSION vector; Type: COMMENT; Schema: -; Owner: -
--



--
-- Name: asset_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.asset_type AS ENUM (
    'poster',
    'backdrop',
    'logo',
    'art',
    'banner',
    'thumb',
    'disc',
    'clearart',
    'subtitle',
    'lyrics',
    'nfo',
    'still'
);


--
-- Name: file_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.file_status AS ENUM (
    'pending',
    'matched',
    'unmatched',
    'ignored',
    'error'
);


--
-- Name: media_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.media_type AS ENUM (
    'movie',
    'tv',
    'music',
    'book',
    'comic',
    'podcast',
    'radio',
    'anime'
);



--
-- Name: immutable_array_to_string(text[], text); Type: FUNCTION; Schema: public; Owner: -
--

-- +goose StatementBegin
CREATE FUNCTION public.immutable_array_to_string(arr text[], sep text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT array_to_string(arr, sep)
$$;
-- +goose StatementEnd



--
-- Name: album_centroids; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.album_centroids (
    album_id bigint NOT NULL,
    sonic_centroid vector(512),
    text_centroid vector(512),
    track_count integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT album_centroids_pkey PRIMARY KEY (album_id)
);


--
-- Name: albums; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.albums (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    artist_id bigint NOT NULL,
    title text NOT NULL,
    slug text DEFAULT ''::text NOT NULL,
    year text DEFAULT ''::text NOT NULL,
    musicbrainz_id text DEFAULT ''::text NOT NULL,
    album_type text DEFAULT 'album'::text NOT NULL,
    genres text[] DEFAULT '{}'::text[] NOT NULL,
    cover_path text DEFAULT ''::text NOT NULL,
    release_date date,
    label text DEFAULT ''::text NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    barcode text DEFAULT ''::text NOT NULL,
    total_tracks integer DEFAULT 0 NOT NULL,
    total_discs integer DEFAULT 0 NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    integrated_lufs numeric(6,2),
    true_peak_db numeric(6,2),
    loudness_range_db numeric(6,2),
    loudness_analyzed_at timestamp with time zone,
    search_vector tsvector GENERATED ALWAYS AS ((setweight(to_tsvector('simple'::regconfig, COALESCE(title, ''::text)), 'A'::"char") || setweight(to_tsvector('simple'::regconfig, public.immutable_array_to_string(COALESCE(tags, '{}'::text[]), ' '::text)), 'C'::"char"))) STORED,
    catalog_no text DEFAULT ''::text NOT NULL,
    explicit boolean DEFAULT false NOT NULL,
    original_title text DEFAULT ''::text NOT NULL,
    secondary_types text[] DEFAULT '{}'::text[] NOT NULL,
    styles text[] DEFAULT '{}'::text[] NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    duration_seconds integer DEFAULT 0 NOT NULL,
    isrcs text[] DEFAULT '{}'::text[] NOT NULL,
    rating numeric(4,2) DEFAULT 0 NOT NULL,
    popularity integer DEFAULT 0 NOT NULL,
    listeners bigint DEFAULT 0 NOT NULL,
    playcount bigint DEFAULT 0 NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    artist_credits jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT albums_pkey PRIMARY KEY (id)
);


--
-- Name: artist_centroids; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.artist_centroids (
    artist_id bigint NOT NULL,
    sonic_centroid vector(512),
    text_centroid vector(512),
    track_count integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT artist_centroids_pkey PRIMARY KEY (artist_id)
);


--
-- Name: artist_similar_artists; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.artist_similar_artists (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    artist_id bigint NOT NULL,
    rank integer NOT NULL,
    name text NOT NULL,
    mbid text DEFAULT ''::text NOT NULL,
    match_score numeric(6,4) DEFAULT 0 NOT NULL,
    url text DEFAULT ''::text NOT NULL,
    local_artist_id bigint,
    CONSTRAINT artist_similar_artists_artist_id_rank_key UNIQUE (artist_id, rank),
    CONSTRAINT artist_similar_artists_pkey PRIMARY KEY (id)
);


--
-- Name: artist_top_tracks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.artist_top_tracks (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    artist_id bigint NOT NULL,
    rank integer NOT NULL,
    title text NOT NULL,
    mbid text DEFAULT ''::text NOT NULL,
    playcount bigint DEFAULT 0 NOT NULL,
    listeners bigint DEFAULT 0 NOT NULL,
    url text DEFAULT ''::text NOT NULL,
    CONSTRAINT artist_top_tracks_artist_id_rank_key UNIQUE (artist_id, rank),
    CONSTRAINT artist_top_tracks_pkey PRIMARY KEY (id)
);


--
-- Name: artists; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.artists (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    musicbrainz_id text DEFAULT ''::text NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    sort_name text DEFAULT ''::text NOT NULL,
    disambiguation text DEFAULT ''::text NOT NULL,
    biography text DEFAULT ''::text NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (((setweight(to_tsvector('simple'::regconfig, COALESCE(name, ''::text)), 'A'::"char") || setweight(to_tsvector('simple'::regconfig, COALESCE(sort_name, ''::text)), 'A'::"char")) || setweight(to_tsvector('english'::regconfig, COALESCE(biography, ''::text)), 'D'::"char"))) STORED,
    discography_enriched_at timestamp with time zone,
    cover_art_enriched_at timestamp with time zone,
    listeners bigint DEFAULT 0 NOT NULL,
    playcount bigint DEFAULT 0 NOT NULL,
    popularity integer DEFAULT 0 NOT NULL,
    annotation text DEFAULT ''::text NOT NULL,
    urls jsonb DEFAULT '[]'::jsonb NOT NULL,
    wikipedia_links jsonb DEFAULT '{}'::jsonb NOT NULL,
    profiles jsonb DEFAULT '{}'::jsonb NOT NULL,
    aliases text[] DEFAULT '{}'::text[] NOT NULL,
    groups jsonb DEFAULT '[]'::jsonb NOT NULL,
    members jsonb DEFAULT '[]'::jsonb NOT NULL,
    artist_type text DEFAULT ''::text NOT NULL,
    begin_date text DEFAULT ''::text NOT NULL,
    begin_year integer DEFAULT 0 NOT NULL,
    end_date text DEFAULT ''::text NOT NULL,
    ended boolean DEFAULT false NOT NULL,
    deathday text DEFAULT ''::text NOT NULL,
    birthplace text DEFAULT ''::text NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT artists_media_item_id_key UNIQUE (media_item_id),
    CONSTRAINT artists_pkey PRIMARY KEY (id)
);


--
-- Name: authors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.authors (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    openlibrary_id text DEFAULT ''::text NOT NULL,
    biography text DEFAULT ''::text NOT NULL,
    birth_date text DEFAULT ''::text NOT NULL,
    death_date text DEFAULT ''::text NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS ((setweight(to_tsvector('simple'::regconfig, COALESCE(name, ''::text)), 'A'::"char") || setweight(to_tsvector('english'::regconfig, COALESCE(biography, ''::text)), 'D'::"char"))) STORED,
    CONSTRAINT authors_pkey PRIMARY KEY (id)
);


--
-- Name: books; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.books (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    author_id bigint,
    isbn text DEFAULT ''::text NOT NULL,
    openlibrary_id text DEFAULT ''::text NOT NULL,
    page_count integer DEFAULT 0 NOT NULL,
    publisher text DEFAULT ''::text NOT NULL,
    publish_date date,
    file_path text DEFAULT ''::text NOT NULL,
    subjects text[] DEFAULT '{}'::text[] NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    series_name text DEFAULT ''::text NOT NULL,
    series_number integer DEFAULT 0 NOT NULL,
    format text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    CONSTRAINT books_media_item_id_key UNIQUE (media_item_id),
    CONSTRAINT books_pkey PRIMARY KEY (id)
);


--
-- Name: collections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.collections (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    overview text DEFAULT ''::text NOT NULL,
    poster_path text DEFAULT ''::text NOT NULL,
    backdrop_path text DEFAULT ''::text NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS ((setweight(to_tsvector('simple'::regconfig, COALESCE(name, ''::text)), 'A'::"char") || setweight(to_tsvector('english'::regconfig, COALESCE(overview, ''::text)), 'D'::"char"))) STORED,
    parts jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT collections_pkey PRIMARY KEY (id)
);


--
-- Name: creators; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.creators (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT creators_name_key UNIQUE (name),
    CONSTRAINT creators_pkey PRIMARY KEY (id)
);


--
-- Name: debounced_enriches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.debounced_enriches (
    media_item_id bigint NOT NULL,
    fire_at timestamp with time zone NOT NULL,
    requested_by text DEFAULT 'matcher'::text NOT NULL,
    CONSTRAINT debounced_enriches_pkey PRIMARY KEY (media_item_id)
);


--
-- Name: episode_overviews; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.episode_overviews (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    episode_id bigint NOT NULL,
    language text NOT NULL,
    overview text DEFAULT ''::text NOT NULL,
    CONSTRAINT episode_overviews_episode_id_language_key UNIQUE (episode_id, language),
    CONSTRAINT episode_overviews_pkey PRIMARY KEY (id)
);


--
-- Name: episode_titles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.episode_titles (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    episode_id bigint NOT NULL,
    title text NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    CONSTRAINT episode_titles_episode_id_language_key UNIQUE (episode_id, language),
    CONSTRAINT episode_titles_pkey PRIMARY KEY (id)
);


--
-- Name: external_ratings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.external_ratings (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    source text NOT NULL,
    value text NOT NULL,
    score numeric(5,1),
    votes integer DEFAULT 0 NOT NULL,
    raw_value text DEFAULT ''::text NOT NULL,
    CONSTRAINT external_ratings_media_item_id_source_key UNIQUE (media_item_id, source),
    CONSTRAINT external_ratings_pkey PRIMARY KEY (id)
);


--
-- Name: keywords; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.keywords (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    CONSTRAINT keywords_pkey PRIMARY KEY (id)
);


--
-- Name: libraries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.libraries (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    media_type public.media_type NOT NULL,
    paths text[] DEFAULT '{}'::text[] NOT NULL,
    scan_interval interval DEFAULT '01:00:00'::interval NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT libraries_pkey PRIMARY KEY (id)
);


--
-- Name: library_disk_usage; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.library_disk_usage (
    library_id bigint NOT NULL,
    path text NOT NULL,
    bytes bigint NOT NULL,
    file_count bigint NOT NULL,
    scanned_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT library_disk_usage_pkey PRIMARY KEY (library_id, path)
);


--
-- Name: library_file_links; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.library_file_links (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_file_id bigint NOT NULL,
    media_item_id bigint NOT NULL,
    movie_id bigint,
    tv_episode_id bigint,
    relation_type text DEFAULT 'primary'::text NOT NULL,
    season_number integer,
    episode_number integer,
    absolute_number integer,
    part_index integer,
    title text DEFAULT ''::text NOT NULL,
    source text DEFAULT 'scanner_v2'::text NOT NULL,
    confidence real DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    identity_id bigint,
    scan_run_id bigint,
    extra_type text DEFAULT ''::text NOT NULL,
    thumbnail_path text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT library_file_links_pkey PRIMARY KEY (id)
);


--
-- Name: library_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.library_files (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_id bigint NOT NULL,
    path text NOT NULL,
    size bigint DEFAULT 0 NOT NULL,
    mtime timestamp with time zone,
    media_item_id bigint,
    parse_result jsonb DEFAULT '{}'::jsonb NOT NULL,
    status public.file_status DEFAULT 'pending'::public.file_status NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    deleted_at timestamp with time zone,
    media_info jsonb DEFAULT '{}'::jsonb NOT NULL,
    keyframes jsonb,
    has_trickplay boolean DEFAULT false NOT NULL,
    content_hash text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    video_height integer DEFAULT 0 NOT NULL,
    segments_analyzed_at timestamp with time zone,
    segments_detected_at timestamp with time zone,
    CONSTRAINT library_files_library_id_path_key UNIQUE (library_id, path),
    CONSTRAINT library_files_pkey PRIMARY KEY (id)
);


--
-- Name: library_nfo_dirs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.library_nfo_dirs (
    library_id bigint NOT NULL,
    dir_path text NOT NULL,
    nfo_name text NOT NULL,
    mtime timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT library_nfo_dirs_pkey PRIMARY KEY (library_id, dir_path)
);


--
-- Name: local_media_identities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.local_media_identities (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_id bigint NOT NULL,
    media_type public.media_type NOT NULL,
    identity_key text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    year text DEFAULT ''::text NOT NULL,
    confidence real DEFAULT 0 NOT NULL,
    source text DEFAULT 'scanner'::text NOT NULL,
    review_status text DEFAULT 'accepted'::text NOT NULL,
    metadata_provider_id text DEFAULT ''::text NOT NULL,
    media_item_id bigint,
    first_seen_scan_run_id bigint,
    last_seen_scan_run_id bigint,
    raw_identity jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT local_media_identities_library_id_media_type_identity_key_key UNIQUE (library_id, media_type, identity_key),
    CONSTRAINT local_media_identities_pkey PRIMARY KEY (id)
);


--
-- Name: local_media_identity_external_ids; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.local_media_identity_external_ids (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    identity_id bigint NOT NULL,
    provider text NOT NULL,
    external_id text NOT NULL,
    source text DEFAULT 'scanner'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT local_media_identity_external_ids_identity_id_provider_key UNIQUE (identity_id, provider),
    CONSTRAINT local_media_identity_external_ids_pkey PRIMARY KEY (id)
);


--
-- Name: match_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.match_candidates (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_file_id bigint NOT NULL,
    provider_name text NOT NULL,
    provider_id text NOT NULL,
    title text NOT NULL,
    year text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    poster_url text DEFAULT ''::text NOT NULL,
    confidence numeric(4,3) NOT NULL,
    raw_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    chosen boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT match_candidates_library_file_id_provider_id_key UNIQUE (library_file_id, provider_id),
    CONSTRAINT match_candidates_pkey PRIMARY KEY (id)
);


--
-- Name: media_assets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_assets (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    asset_type public.asset_type NOT NULL,
    source text DEFAULT 'local'::text NOT NULL,
    local_path text DEFAULT ''::text NOT NULL,
    remote_url text DEFAULT ''::text NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    label text DEFAULT ''::text NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    width integer DEFAULT 0 NOT NULL,
    height integer DEFAULT 0 NOT NULL,
    file_size bigint DEFAULT 0 NOT NULL,
    score numeric(8,3) DEFAULT 0 NOT NULL,
    likes integer DEFAULT 0 NOT NULL,
    aspect text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT media_assets_pkey PRIMARY KEY (id)
);


--
-- Name: media_cast; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_cast (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    person_id bigint NOT NULL,
    "character" text DEFAULT ''::text NOT NULL,
    display_order integer DEFAULT 0 NOT NULL,
    gender integer DEFAULT 0 NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    CONSTRAINT media_cast_media_item_id_person_id_character_key UNIQUE (media_item_id, person_id, "character"),
    CONSTRAINT media_cast_pkey PRIMARY KEY (id)
);


--
-- Name: media_certifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_certifications (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    certification text DEFAULT ''::text NOT NULL,
    release_date date,
    release_type integer DEFAULT 0 NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    CONSTRAINT media_certifications_media_item_id_country_release_type_key UNIQUE (media_item_id, country, release_type),
    CONSTRAINT media_certifications_pkey PRIMARY KEY (id)
);


--
-- Name: media_crew; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_crew (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    person_id bigint NOT NULL,
    job text DEFAULT ''::text NOT NULL,
    department text DEFAULT ''::text NOT NULL,
    gender integer DEFAULT 0 NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    CONSTRAINT media_crew_media_item_id_person_id_job_key UNIQUE (media_item_id, person_id, job),
    CONSTRAINT media_crew_pkey PRIMARY KEY (id)
);


--
-- Name: media_item_external_ids; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_item_external_ids (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    library_id bigint NOT NULL,
    provider text NOT NULL,
    external_id text NOT NULL,
    source text DEFAULT 'metadata'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT media_item_external_ids_media_item_id_provider_key UNIQUE (media_item_id, provider),
    CONSTRAINT media_item_external_ids_pkey PRIMARY KEY (id)
);


--
-- Name: media_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_items (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_id bigint NOT NULL,
    media_type public.media_type NOT NULL,
    slug text DEFAULT ''::text NOT NULL,
    provider_kind text DEFAULT ''::text NOT NULL,
    heya_slug text DEFAULT ''::text NOT NULL,
    heya_enriched_at timestamp with time zone,
    metadata_refreshed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    matched_at timestamp with time zone,
    enrichment_status text DEFAULT 'pending'::text NOT NULL,
    base_enriched_at timestamp with time zone,
    people_enriched_at timestamp with time zone,
    extras_enriched_at timestamp with time zone,
    images_enriched_at timestamp with time zone,
    structure_enriched_at timestamp with time zone,
    last_enrich_attempt_at timestamp with time zone,
    last_enrich_error text DEFAULT ''::text NOT NULL,
    field_provenance jsonb DEFAULT '{}'::jsonb NOT NULL,
    match_confidence real DEFAULT 0 NOT NULL,
    slug_locked boolean DEFAULT false NOT NULL,
    CONSTRAINT media_items_pkey PRIMARY KEY (id)
);


--
-- Name: media_item_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_item_profiles (
    media_item_id bigint NOT NULL,
    title text NOT NULL,
    sort_title text DEFAULT ''::text NOT NULL,
    year text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    poster_path text DEFAULT ''::text NOT NULL,
    backdrop_path text DEFAULT ''::text NOT NULL,
    homepage text DEFAULT ''::text NOT NULL,
    tagline text DEFAULT ''::text NOT NULL,
    original_title text DEFAULT ''::text NOT NULL,
    original_language text DEFAULT ''::text NOT NULL,
    status text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('english'::regconfig, ((title || ' '::text) || COALESCE(description, ''::text)))) STORED,
    CONSTRAINT media_item_profiles_pkey PRIMARY KEY (media_item_id)
);


--
-- Name: media_item_cards; Type: VIEW; Schema: public; Owner: -
--

CREATE VIEW public.media_item_cards AS
 SELECT e.id,
    e.library_id,
    e.media_type,
    COALESCE(p.title, ''::text) AS title,
    COALESCE(p.sort_title, ''::text) AS sort_title,
    COALESCE(p.year, ''::text) AS year,
    COALESCE(p.description, ''::text) AS description,
    COALESCE(p.poster_path, ''::text) AS poster_path,
    COALESCE(p.backdrop_path, ''::text) AS backdrop_path,
    COALESCE(ext.external_ids, '{}'::jsonb) AS external_ids,
    e.slug,
    COALESCE(p.homepage, ''::text) AS homepage,
    COALESCE(p.tagline, ''::text) AS tagline,
    COALESCE(p.original_title, ''::text) AS original_title,
    COALESCE(p.original_language, ''::text) AS original_language,
    COALESCE(p.status, ''::text) AS status,
    e.provider_kind,
    e.heya_slug,
    e.heya_enriched_at,
    e.metadata_refreshed_at,
    e.created_at,
    GREATEST(e.updated_at, COALESCE(p.updated_at, e.updated_at))::timestamp with time zone AS updated_at,
    p.search_vector,
    e.matched_at,
    e.enrichment_status,
    e.base_enriched_at,
    e.people_enriched_at,
    e.extras_enriched_at,
    e.images_enriched_at,
    e.structure_enriched_at,
    e.last_enrich_attempt_at,
    e.last_enrich_error,
    e.field_provenance,
    e.match_confidence,
    e.slug_locked
   FROM public.media_items e
     LEFT JOIN public.media_item_profiles p ON p.media_item_id = e.id
     LEFT JOIN LATERAL (
        SELECT jsonb_object_agg(ei.provider, ei.external_id ORDER BY ei.provider) AS external_ids
          FROM public.media_item_external_ids ei
         WHERE ei.media_item_id = e.id
     ) ext ON true;


--
-- Name: media_keywords; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_keywords (
    media_item_id bigint NOT NULL,
    keyword_id bigint NOT NULL,
    CONSTRAINT media_keywords_pkey PRIMARY KEY (media_item_id, keyword_id)
);


--
-- Name: media_overviews; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_overviews (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    language text NOT NULL,
    overview text DEFAULT ''::text NOT NULL,
    CONSTRAINT media_overviews_media_item_id_language_key UNIQUE (media_item_id, language),
    CONSTRAINT media_overviews_pkey PRIMARY KEY (id)
);


--
-- Name: media_production_companies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_production_companies (
    media_item_id bigint NOT NULL,
    company_id bigint NOT NULL,
    CONSTRAINT media_production_companies_pkey PRIMARY KEY (media_item_id, company_id)
);


--
-- Name: media_recommendations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_recommendations (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    poster_path text DEFAULT ''::text NOT NULL,
    media_type text DEFAULT ''::text NOT NULL,
    vote_average numeric(3,1) DEFAULT 0 NOT NULL,
    release_date text DEFAULT ''::text NOT NULL,
    CONSTRAINT media_recommendations_media_item_id_title_media_type_key UNIQUE (media_item_id, title, media_type),
    CONSTRAINT media_recommendations_pkey PRIMARY KEY (id)
);


--
-- Name: media_segments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_segments (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_file_id bigint NOT NULL,
    segment_type text NOT NULL,
    start_ms bigint NOT NULL,
    end_ms bigint NOT NULL,
    source text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT media_segments_pkey PRIMARY KEY (id)
);


--
-- Name: media_titles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_titles (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    title text NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    title_type text DEFAULT 'translation'::text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    CONSTRAINT media_titles_media_item_id_title_language_key UNIQUE (media_item_id, title, language),
    CONSTRAINT media_titles_pkey PRIMARY KEY (id)
);


--
-- Name: media_videos; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_videos (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    provider_key text DEFAULT ''::text NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    site text DEFAULT ''::text NOT NULL,
    video_key text DEFAULT ''::text NOT NULL,
    video_type text DEFAULT ''::text NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    official boolean DEFAULT false NOT NULL,
    published_at timestamp with time zone,
    CONSTRAINT media_videos_media_item_id_video_key_key UNIQUE (media_item_id, video_key),
    CONSTRAINT media_videos_pkey PRIMARY KEY (id)
);


--
-- Name: metadata_match_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.metadata_match_candidates (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    identity_id bigint NOT NULL,
    scan_run_id bigint,
    provider_name text DEFAULT 'heya'::text NOT NULL,
    provider_id text NOT NULL,
    provider_kind text DEFAULT ''::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    year text DEFAULT ''::text NOT NULL,
    score numeric(6,3) DEFAULT 0 NOT NULL,
    rank integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'candidate'::text NOT NULL,
    rejection_reason text DEFAULT ''::text NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    raw_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT metadata_match_candidates_identity_id_provider_id_key UNIQUE (identity_id, provider_id),
    CONSTRAINT metadata_match_candidates_pkey PRIMARY KEY (id)
);


--
-- Name: movies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.movies (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    runtime_minutes integer DEFAULT 0 NOT NULL,
    tagline text DEFAULT ''::text NOT NULL,
    genres text[] DEFAULT '{}'::text[] NOT NULL,
    rating numeric(5,2) DEFAULT 0 NOT NULL,
    release_date date,
    original_title text DEFAULT ''::text NOT NULL,
    original_language text DEFAULT ''::text NOT NULL,
    budget bigint DEFAULT 0 NOT NULL,
    revenue bigint DEFAULT 0 NOT NULL,
    popularity numeric(10,3) DEFAULT 0 NOT NULL,
    collection_id bigint,
    status text DEFAULT ''::text NOT NULL,
    homepage text DEFAULT ''::text NOT NULL,
    spoken_languages text[] DEFAULT '{}'::text[] NOT NULL,
    origin_country text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT movies_media_item_id_key UNIQUE (media_item_id),
    CONSTRAINT movies_pkey PRIMARY KEY (id)
);


--
-- Name: networks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.networks (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    logo_path text DEFAULT ''::text NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    CONSTRAINT networks_name_key UNIQUE (name),
    CONSTRAINT networks_pkey PRIMARY KEY (id)
);


--
-- Name: people; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.people (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    also_known_as text[] DEFAULT '{}'::text[] NOT NULL,
    biography text DEFAULT ''::text NOT NULL,
    birthday text DEFAULT ''::text NOT NULL,
    deathday text DEFAULT ''::text NOT NULL,
    place_of_birth text DEFAULT ''::text NOT NULL,
    gender integer DEFAULT 0 NOT NULL,
    profile_path text DEFAULT ''::text NOT NULL,
    homepage text DEFAULT ''::text NOT NULL,
    popularity numeric(10,3) DEFAULT 0 NOT NULL,
    slug text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    sort_name text DEFAULT ''::text NOT NULL,
    known_for_department text DEFAULT ''::text NOT NULL,
    birth_year integer DEFAULT 0 NOT NULL,
    heya_slug text DEFAULT ''::text NOT NULL,
    heya_enriched_at timestamp with time zone,
    search_vector tsvector GENERATED ALWAYS AS (((setweight(to_tsvector('simple'::regconfig, COALESCE(name, ''::text)), 'A'::"char") || setweight(to_tsvector('simple'::regconfig, public.immutable_array_to_string(COALESCE(also_known_as, '{}'::text[]), ' '::text)), 'B'::"char")) || setweight(to_tsvector('english'::regconfig, COALESCE(biography, ''::text)), 'D'::"char"))) STORED,
    CONSTRAINT people_pkey PRIMARY KEY (id)
);


--
-- Name: person_biographies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.person_biographies (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    person_id bigint NOT NULL,
    language text NOT NULL,
    biography text DEFAULT ''::text NOT NULL,
    CONSTRAINT person_biographies_person_id_language_key UNIQUE (person_id, language),
    CONSTRAINT person_biographies_pkey PRIMARY KEY (id)
);


--
-- Name: person_external_credits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.person_external_credits (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    person_id bigint NOT NULL,
    kind text NOT NULL,
    media_kind text DEFAULT ''::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    year integer DEFAULT 0 NOT NULL,
    "character" text DEFAULT ''::text NOT NULL,
    job text DEFAULT ''::text NOT NULL,
    department text DEFAULT ''::text NOT NULL,
    episode_count integer DEFAULT 0 NOT NULL,
    display_order integer DEFAULT 0 NOT NULL,
    slug text DEFAULT ''::text NOT NULL,
    poster_url text DEFAULT ''::text NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT person_external_credits_kind_check CHECK ((kind = ANY (ARRAY['cast'::text, 'crew'::text, 'known_for'::text]))),
    CONSTRAINT person_external_credits_person_id_kind_title_year_character_key UNIQUE (person_id, kind, title, year, "character", job),
    CONSTRAINT person_external_credits_pkey PRIMARY KEY (id)
);


--
-- Name: person_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.person_profiles (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    person_id bigint NOT NULL,
    url text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    aspect text DEFAULT 'profile'::text NOT NULL,
    width integer DEFAULT 0 NOT NULL,
    height integer DEFAULT 0 NOT NULL,
    score numeric(8,3) DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    CONSTRAINT person_profiles_person_id_url_key UNIQUE (person_id, url),
    CONSTRAINT person_profiles_pkey PRIMARY KEY (id)
);


--
-- Name: play_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.play_events (
    id bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    track_id bigint NOT NULL,
    played_at timestamp with time zone DEFAULT now() NOT NULL,
    listened_seconds integer NOT NULL,
    completed boolean DEFAULT false NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    CONSTRAINT play_events_pkey PRIMARY KEY (id)
);


--
-- Name: production_companies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.production_companies (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    logo_path text DEFAULT ''::text NOT NULL,
    origin_country text DEFAULT ''::text NOT NULL,
    CONSTRAINT production_companies_pkey PRIMARY KEY (id)
);


--
-- Name: scan_findings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scan_findings (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    scan_run_id bigint,
    library_id bigint NOT NULL,
    media_type public.media_type NOT NULL,
    identity_id bigint,
    media_item_id bigint,
    library_file_id bigint,
    severity text DEFAULT 'info'::text NOT NULL,
    code text NOT NULL,
    rel_path text DEFAULT ''::text NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT scan_findings_pkey PRIMARY KEY (id)
);


--
-- Name: scan_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scan_runs (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_id bigint NOT NULL,
    media_type public.media_type NOT NULL,
    scanner_version text DEFAULT 'v2'::text NOT NULL,
    mode text DEFAULT 'scan'::text NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    summary jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT scan_runs_pkey PRIMARY KEY (id)
);


--
-- Name: scheduled_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scheduled_tasks (
    id text NOT NULL,
    display_name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    category text DEFAULT 'media'::text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    interval_hours integer DEFAULT 24 NOT NULL,
    daily_start_time text DEFAULT '02:00'::text NOT NULL,
    daily_end_time text DEFAULT '06:00'::text NOT NULL,
    max_runtime_minutes integer DEFAULT 120 NOT NULL,
    last_run_at timestamp with time zone,
    last_run_result text DEFAULT ''::text NOT NULL,
    last_run_duration_sec integer DEFAULT 0 NOT NULL,
    last_run_items_processed integer DEFAULT 0 NOT NULL,
    last_run_items_total integer DEFAULT 0 NOT NULL,
    next_run_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT scheduled_tasks_pkey PRIMARY KEY (id)
);


--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    token_hash text NOT NULL,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    kind text DEFAULT 'session'::text NOT NULL,
    name text,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    user_agent text,
    ip text,
    CONSTRAINT sessions_kind_check CHECK ((kind = ANY (ARRAY['session'::text, 'api_token'::text]))),
    CONSTRAINT sessions_pkey PRIMARY KEY (id),
    CONSTRAINT sessions_token_hash_key UNIQUE (token_hash)
);


--
-- Name: system_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_settings (
    key text NOT NULL,
    value jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT system_settings_pkey PRIMARY KEY (key)
);


--
-- Name: thumbnail_eligible_extras; Type: VIEW; Schema: public; Owner: -
--

CREATE VIEW public.thumbnail_eligible_extras AS
 SELECT lfl.id,
    COALESCE(NULLIF(lfl.title, ''::text), regexp_replace(regexp_replace(lf.path, '^.*/'::text, ''::text), '\.[^.]*$'::text, ''::text))::text AS title,
    lf.path AS file_path,
    lfl.thumbnail_path,
    COALESCE(NULLIF(lfl.extra_type, ''::text), 'other'::text) AS extra_type,
    mi.title::text AS media_title
   FROM (((public.library_file_links lfl
     JOIN public.library_files lf ON ((lf.id = lfl.library_file_id)))
     JOIN public.media_item_cards mi ON ((mi.id = lfl.media_item_id)))
     JOIN public.libraries l ON ((l.id = mi.library_id)))
  WHERE ((lfl.relation_type = 'extra'::text) AND (lf.deleted_at IS NULL) AND (lf.path <> ''::text) AND ((l.settings ->> 'generate_thumbnails'::text) = 'true'::text));


--
-- Name: track_facets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.track_facets (
    track_id bigint NOT NULL,
    track_embedding vector(512),
    artist_embedding vector(512),
    release_embedding vector(512),
    text_embedding vector(512),
    bpm real,
    bpm_confidence real,
    key_root smallint,
    key_mode smallint,
    key_clarity real,
    top_genres jsonb,
    mood_tags jsonb,
    waveform real[],
    analyzed_at timestamp with time zone DEFAULT now() NOT NULL,
    analyzer_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT track_facets_pkey PRIMARY KEY (track_id)
);


--
-- Name: track_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.track_files (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    track_id bigint NOT NULL,
    library_file_id bigint NOT NULL,
    format text DEFAULT ''::text NOT NULL,
    quality_score integer DEFAULT 0 NOT NULL,
    bitrate_kbps integer DEFAULT 0 NOT NULL,
    sample_rate_hz integer DEFAULT 0 NOT NULL,
    bit_depth integer DEFAULT 0 NOT NULL,
    channels integer DEFAULT 0 NOT NULL,
    duration integer DEFAULT 0 NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    lyrics_path text DEFAULT ''::text NOT NULL,
    integrated_lufs numeric(6,2),
    true_peak_db numeric(6,2),
    loudness_range_db numeric(6,2),
    sample_peak_db numeric(6,2),
    loudness_analyzed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    intro_end_ms integer,
    outro_start_ms integer,
    fade_start_ms integer,
    silence_start_ms integer,
    boundaries_analyzed_at timestamp with time zone,
    chromaprint text,
    chromaprint_algorithm smallint,
    chromaprint_duration_secs integer,
    fingerprinted_at timestamp with time zone,
    CONSTRAINT track_files_library_file_id_key UNIQUE (library_file_id),
    CONSTRAINT track_files_pkey PRIMARY KEY (id)
);


--
-- Name: tracks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tracks (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    album_id bigint NOT NULL,
    disc_number integer DEFAULT 1 NOT NULL,
    track_number integer NOT NULL,
    title text NOT NULL,
    duration integer DEFAULT 0 NOT NULL,
    file_path text DEFAULT ''::text NOT NULL,
    lyrics_path text DEFAULT ''::text NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple'::regconfig, COALESCE(title, ''::text))) STORED,
    library_file_id bigint,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    isrc text DEFAULT ''::text NOT NULL,
    recording_mbid text DEFAULT ''::text NOT NULL,
    preview_url text DEFAULT ''::text NOT NULL,
    explicit boolean DEFAULT false NOT NULL,
    artist_credits jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT tracks_album_id_disc_number_track_number_key UNIQUE (album_id, disc_number, track_number),
    CONSTRAINT tracks_pkey PRIMARY KEY (id)
);


--
-- Name: trickplay_eligible_files; Type: VIEW; Schema: public; Owner: -
--

CREATE VIEW public.trickplay_eligible_files AS
 SELECT lf.id,
    lf.path,
    lf.has_trickplay
   FROM (public.library_files lf
     JOIN public.libraries l ON ((l.id = lf.library_id)))
  WHERE ((lf.deleted_at IS NULL) AND (lf.status = 'matched'::public.file_status) AND (lf.media_info IS NOT NULL) AND ((lf.media_info -> 'streams'::text) @> '[{"codec_type": "video"}]'::jsonb) AND ((l.settings ->> 'enable_trickplay'::text) = 'true'::text));


--
-- Name: tv_episodes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tv_episodes (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    season_id bigint NOT NULL,
    episode_number integer NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    overview text DEFAULT ''::text NOT NULL,
    still_path text DEFAULT ''::text NOT NULL,
    runtime_minutes integer DEFAULT 0 NOT NULL,
    air_date date,
    rating numeric(5,2) DEFAULT 0 NOT NULL,
    absolute_number integer DEFAULT 0 NOT NULL,
    is_special boolean DEFAULT false NOT NULL,
    episode_type integer DEFAULT 1 NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    CONSTRAINT tv_episodes_pkey PRIMARY KEY (id),
    CONSTRAINT tv_episodes_season_id_episode_number_key UNIQUE (season_id, episode_number)
);


--
-- Name: tv_seasons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tv_seasons (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    series_id bigint NOT NULL,
    season_number integer NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    overview text DEFAULT ''::text NOT NULL,
    poster_path text DEFAULT ''::text NOT NULL,
    air_date date,
    end_date date,
    status text DEFAULT ''::text NOT NULL,
    aired_episodes integer DEFAULT 0 NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT tv_seasons_pkey PRIMARY KEY (id),
    CONSTRAINT tv_seasons_series_id_season_number_key UNIQUE (series_id, season_number)
);


--
-- Name: tv_series; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tv_series (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    media_item_id bigint NOT NULL,
    status text DEFAULT ''::text NOT NULL,
    genres text[] DEFAULT '{}'::text[] NOT NULL,
    rating numeric(5,2) DEFAULT 0 NOT NULL,
    first_air_date date,
    last_air_date date,
    original_name text DEFAULT ''::text NOT NULL,
    original_language text DEFAULT ''::text NOT NULL,
    number_of_seasons integer DEFAULT 0 NOT NULL,
    number_of_episodes integer DEFAULT 0 NOT NULL,
    popularity numeric(10,3) DEFAULT 0 NOT NULL,
    spoken_languages text[] DEFAULT '{}'::text[] NOT NULL,
    origin_country text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT tv_series_media_item_id_key UNIQUE (media_item_id),
    CONSTRAINT tv_series_pkey PRIMARY KEY (id)
);


--
-- Name: tv_series_creators; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tv_series_creators (
    series_id bigint NOT NULL,
    creator_id bigint NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    CONSTRAINT tv_series_creators_pkey PRIMARY KEY (series_id, creator_id)
);


--
-- Name: tv_series_networks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tv_series_networks (
    series_id bigint NOT NULL,
    network_id bigint NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    CONSTRAINT tv_series_networks_pkey PRIMARY KEY (series_id, network_id)
);


--
-- Name: user_album_ratings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_album_ratings (
    user_id bigint NOT NULL,
    album_id bigint NOT NULL,
    rating smallint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_album_ratings_rating_check CHECK (((rating >= 1) AND (rating <= 10))),
    CONSTRAINT user_album_ratings_pkey PRIMARY KEY (user_id, album_id)
);


--
-- Name: user_artist_ratings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_artist_ratings (
    user_id bigint NOT NULL,
    artist_id bigint NOT NULL,
    rating smallint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_artist_ratings_rating_check CHECK (((rating >= 1) AND (rating <= 10))),
    CONSTRAINT user_artist_ratings_pkey PRIMARY KEY (user_id, artist_id)
);


--
-- Name: user_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_favorites (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    entity_type text NOT NULL,
    entity_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_favorites_pkey PRIMARY KEY (id),
    CONSTRAINT user_favorites_user_id_entity_type_entity_id_key UNIQUE (user_id, entity_type, entity_id)
);


--
-- Name: user_list_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_list_items (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    list_id bigint NOT NULL,
    media_item_id bigint NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    added_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_list_items_list_id_media_item_id_key UNIQUE (list_id, media_item_id),
    CONSTRAINT user_list_items_pkey PRIMARY KEY (id)
);


--
-- Name: user_lists; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_lists (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    list_type text DEFAULT 'manual'::text NOT NULL,
    filter_json jsonb,
    media_type text DEFAULT ''::text NOT NULL,
    icon text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_lists_pkey PRIMARY KEY (id),
    CONSTRAINT user_lists_user_id_name_key UNIQUE (user_id, name)
);


--
-- Name: user_playback_preferences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_playback_preferences (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    media_item_id bigint NOT NULL,
    audio_language text DEFAULT ''::text NOT NULL,
    subtitle_language text DEFAULT ''::text NOT NULL,
    subtitle_mode text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_playback_preferences_pkey PRIMARY KEY (id),
    CONSTRAINT user_playback_preferences_user_id_media_item_id_key UNIQUE (user_id, media_item_id)
);


--
-- Name: user_playlist_tracks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_playlist_tracks (
    playlist_id bigint NOT NULL,
    track_id bigint NOT NULL,
    "position" integer NOT NULL,
    added_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_playlist_tracks_pkey PRIMARY KEY (playlist_id, track_id)
);


--
-- Name: user_playlists; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_playlists (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    cover_path text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_playlists_pkey PRIMARY KEY (id)
);


--
-- Name: user_podcast_progress; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_podcast_progress (
    id bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    feed_url text NOT NULL,
    episode_guid text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    artwork_url text DEFAULT ''::text NOT NULL,
    audio_url text DEFAULT ''::text NOT NULL,
    progress_seconds integer DEFAULT 0 NOT NULL,
    total_seconds integer DEFAULT 0 NOT NULL,
    completed boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_podcast_progress_pkey PRIMARY KEY (id),
    CONSTRAINT user_podcast_progress_user_id_feed_url_episode_guid_key UNIQUE (user_id, feed_url, episode_guid)
);


--
-- Name: user_podcast_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_podcast_subscriptions (
    id bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    feed_url text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    author text DEFAULT ''::text NOT NULL,
    artwork_url text DEFAULT ''::text NOT NULL,
    last_episode_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_podcast_subscriptions_pkey PRIMARY KEY (id),
    CONSTRAINT user_podcast_subscriptions_user_id_feed_url_key UNIQUE (user_id, feed_url)
);


--
-- Name: user_radio_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_radio_favorites (
    id bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    stationuuid text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    favicon text DEFAULT ''::text NOT NULL,
    homepage text DEFAULT ''::text NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    countrycode text DEFAULT ''::text NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    tags text DEFAULT ''::text NOT NULL,
    codec text DEFAULT ''::text NOT NULL,
    bitrate integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_radio_favorites_pkey PRIMARY KEY (id),
    CONSTRAINT user_radio_favorites_user_id_stationuuid_key UNIQUE (user_id, stationuuid)
);


--
-- Name: user_radio_recents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_radio_recents (
    id bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    stationuuid text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    favicon text DEFAULT ''::text NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    tags text DEFAULT ''::text NOT NULL,
    codec text DEFAULT ''::text NOT NULL,
    bitrate integer DEFAULT 0 NOT NULL,
    played_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_radio_recents_pkey PRIMARY KEY (id)
);


--
-- Name: user_track_ratings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_track_ratings (
    user_id bigint NOT NULL,
    track_id bigint NOT NULL,
    rating smallint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_track_ratings_rating_check CHECK (((rating >= 1) AND (rating <= 10))),
    CONSTRAINT user_track_ratings_pkey PRIMARY KEY (user_id, track_id)
);


--
-- Name: user_watch_progress; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_watch_progress (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    entity_type text NOT NULL,
    entity_id bigint NOT NULL,
    progress_seconds integer DEFAULT 0 NOT NULL,
    total_seconds integer DEFAULT 0 NOT NULL,
    completed boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_watch_progress_pkey PRIMARY KEY (id),
    CONSTRAINT user_watch_progress_user_id_entity_type_entity_id_key UNIQUE (user_id, entity_type, entity_id)
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    username text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    is_admin boolean DEFAULT false NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    favorites_threshold smallint DEFAULT 7 NOT NULL,
    CONSTRAINT users_favorites_threshold_check CHECK (((favorites_threshold >= 1) AND (favorites_threshold <= 10))),
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_pkey PRIMARY KEY (id),
    CONSTRAINT users_username_key UNIQUE (username)
);


--
-- Name: album_centroids_sonic_hnsw; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX album_centroids_sonic_hnsw ON public.album_centroids USING hnsw (sonic_centroid vector_cosine_ops);


--
-- Name: artist_centroids_sonic_hnsw; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX artist_centroids_sonic_hnsw ON public.artist_centroids USING hnsw (sonic_centroid vector_cosine_ops);


--
-- Name: idx_albums_artist_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_artist_id ON public.albums USING btree (artist_id);


--
-- Name: idx_albums_external_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_external_ids ON public.albums USING gin (external_ids);


--
-- Name: idx_albums_lower_label; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_lower_label ON public.albums USING btree (lower(label));


--
-- Name: idx_albums_musicbrainz_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_musicbrainz_id ON public.albums USING btree (musicbrainz_id) WHERE (musicbrainz_id <> ''::text);


--
-- Name: idx_albums_release_month_day; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_release_month_day ON public.albums USING btree (EXTRACT(month FROM release_date), EXTRACT(day FROM release_date), release_date) WHERE (release_date IS NOT NULL);


--
-- Name: idx_albums_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_search ON public.albums USING gin (search_vector);


--
-- Name: idx_albums_title_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_title_trgm ON public.albums USING gin (lower(title) public.gin_trgm_ops);


--
-- Name: idx_albums_year_prefix; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_albums_year_prefix ON public.albums USING btree (((SUBSTRING(year FROM 1 FOR 4))::integer)) WHERE (year ~ '^[0-9]{4}'::text);


--
-- Name: idx_artist_similar_artist; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artist_similar_artist ON public.artist_similar_artists USING btree (artist_id, rank);


--
-- Name: idx_artist_top_tracks_artist; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artist_top_tracks_artist ON public.artist_top_tracks USING btree (artist_id, rank);


--
-- Name: idx_artist_top_tracks_mbid; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artist_top_tracks_mbid ON public.artist_top_tracks USING btree (mbid) WHERE (mbid <> ''::text);


--
-- Name: idx_artists_discography_enriched_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artists_discography_enriched_at ON public.artists USING btree (discography_enriched_at NULLS FIRST);


--
-- Name: idx_artists_musicbrainz_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artists_musicbrainz_id ON public.artists USING btree (musicbrainz_id) WHERE (musicbrainz_id <> ''::text);


--
-- Name: idx_artists_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artists_name_trgm ON public.artists USING gin (lower(name) public.gin_trgm_ops);


--
-- Name: idx_artists_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artists_search ON public.artists USING gin (search_vector);


--
-- Name: idx_artists_sort_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_artists_sort_name_trgm ON public.artists USING gin (lower(sort_name) public.gin_trgm_ops);


--
-- Name: idx_authors_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_authors_name_trgm ON public.authors USING gin (lower(name) public.gin_trgm_ops);


--
-- Name: idx_authors_openlibrary_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_authors_openlibrary_id ON public.authors USING btree (openlibrary_id) WHERE (openlibrary_id <> ''::text);


--
-- Name: idx_authors_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_authors_search ON public.authors USING gin (search_vector);


--
-- Name: idx_books_author_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_books_author_id ON public.books USING btree (author_id);


--
-- Name: idx_books_isbn; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_books_isbn ON public.books USING btree (isbn) WHERE (isbn <> ''::text);


--
-- Name: idx_collections_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_collections_name_trgm ON public.collections USING gin (lower(name) public.gin_trgm_ops);


--
-- Name: idx_collections_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_collections_search ON public.collections USING gin (search_vector);


--
-- Name: idx_creators_external_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_creators_external_ids ON public.creators USING gin (external_ids);


--
-- Name: idx_debounced_enriches_fire_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_debounced_enriches_fire_at ON public.debounced_enriches USING btree (fire_at);


--
-- Name: idx_episode_overviews_episode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_episode_overviews_episode ON public.episode_overviews USING btree (episode_id);


--
-- Name: idx_episode_titles_episode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_episode_titles_episode ON public.episode_titles USING btree (episode_id);


--
-- Name: idx_external_ratings_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_external_ratings_media ON public.external_ratings USING btree (media_item_id);


--
-- Name: idx_library_file_links_episode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_episode ON public.library_file_links USING btree (tv_episode_id) WHERE (tv_episode_id IS NOT NULL);


--
-- Name: idx_library_file_links_extra; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_extra ON public.library_file_links USING btree (media_item_id, extra_type) WHERE (relation_type = 'extra'::text);


--
-- Name: idx_library_file_links_file; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_file ON public.library_file_links USING btree (library_file_id);


--
-- Name: idx_library_file_links_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_identity ON public.library_file_links USING btree (identity_id) WHERE (identity_id IS NOT NULL);


--
-- Name: idx_library_file_links_media_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_media_item ON public.library_file_links USING btree (media_item_id);


--
-- Name: idx_library_file_links_movie; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_movie ON public.library_file_links USING btree (movie_id) WHERE (movie_id IS NOT NULL);


--
-- Name: idx_library_file_links_relation; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_relation ON public.library_file_links USING btree (media_item_id, relation_type);


--
-- Name: idx_library_file_links_scan_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_file_links_scan_run ON public.library_file_links USING btree (scan_run_id) WHERE (scan_run_id IS NOT NULL);


--
-- Name: idx_library_files_content_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_content_hash ON public.library_files USING btree (library_id, content_hash) WHERE (content_hash <> ''::text);


--
-- Name: idx_library_files_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_created_at ON public.library_files USING btree (created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_library_files_deleted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_deleted ON public.library_files USING btree (deleted_at) WHERE (deleted_at IS NOT NULL);


--
-- Name: idx_library_files_lib_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_lib_status ON public.library_files USING btree (library_id, status) WHERE (deleted_at IS NULL);


--
-- Name: idx_library_files_library_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_library_id ON public.library_files USING btree (library_id);


--
-- Name: idx_library_files_media_item_height; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_media_item_height ON public.library_files USING btree (media_item_id, video_height) WHERE (deleted_at IS NULL);


--
-- Name: idx_library_files_media_item_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_media_item_id ON public.library_files USING btree (media_item_id);


--
-- Name: idx_library_files_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_library_files_status ON public.library_files USING btree (status);


--
-- Name: idx_local_media_identities_media_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_local_media_identities_media_item ON public.local_media_identities USING btree (media_item_id) WHERE (media_item_id IS NOT NULL);


--
-- Name: idx_local_media_identities_review; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_local_media_identities_review ON public.local_media_identities USING btree (library_id, review_status);


--
-- Name: idx_local_media_identity_external_ids_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_local_media_identity_external_ids_provider ON public.local_media_identity_external_ids USING btree (provider, external_id);


--
-- Name: idx_match_candidates_confidence; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_match_candidates_confidence ON public.match_candidates USING btree (confidence DESC);


--
-- Name: idx_match_candidates_file; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_match_candidates_file ON public.match_candidates USING btree (library_file_id);


--
-- Name: idx_media_assets_media_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_assets_media_item ON public.media_assets USING btree (media_item_id);


--
-- Name: idx_media_assets_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_assets_type ON public.media_assets USING btree (media_item_id, asset_type);


--
-- Name: idx_media_assets_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_assets_unique ON public.media_assets USING btree (media_item_id, asset_type, sort_order, local_path);


--
-- Name: idx_media_cast_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_cast_media ON public.media_cast USING btree (media_item_id);


--
-- Name: idx_media_cast_person; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_cast_person ON public.media_cast USING btree (person_id);


--
-- Name: idx_media_cert_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_cert_media ON public.media_certifications USING btree (media_item_id);


--
-- Name: idx_media_crew_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_crew_media ON public.media_crew USING btree (media_item_id);


--
-- Name: idx_media_crew_person; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_crew_person ON public.media_crew USING btree (person_id);


--
-- Name: idx_media_item_external_ids_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_item_external_ids_item ON public.media_item_external_ids USING btree (media_item_id);


--
-- Name: idx_media_item_external_ids_provider_external; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_item_external_ids_provider_external ON public.media_item_external_ids USING btree (provider, external_id);


--
-- Name: idx_media_items_enrichment_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_enrichment_pending ON public.media_items USING btree (media_type, metadata_refreshed_at NULLS FIRST) WHERE (enrichment_status <> 'complete'::text);


--
-- Name: idx_media_items_enrichment_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_enrichment_status ON public.media_items USING btree (library_id, enrichment_status);


--
-- Name: idx_media_items_external_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_item_external_ids_external ON public.media_item_external_ids USING btree (external_id);


--
-- Name: idx_media_items_heya_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_items_heya_slug ON public.media_items USING btree (library_id, heya_slug) WHERE (heya_slug <> ''::text);


--
-- Name: idx_media_items_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_identity ON public.media_item_profiles USING btree (year, lower(btrim(title)));


--
-- Name: idx_media_items_imdb_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_items_imdb_unique ON public.media_item_external_ids USING btree (library_id, external_id) WHERE (provider = 'imdb'::text);


--
-- Name: idx_media_items_library_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_library_id ON public.media_items USING btree (library_id);


--
-- Name: idx_media_items_mbid_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_items_mbid_unique ON public.media_item_external_ids USING btree (library_id, external_id) WHERE (provider = 'mbid'::text);


--
-- Name: idx_media_items_media_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_media_type ON public.media_items USING btree (media_type);


--
-- Name: idx_media_items_ol_work_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_items_ol_work_id_unique ON public.media_item_external_ids USING btree (library_id, external_id) WHERE (provider = 'ol_work_id'::text);


--
-- Name: idx_media_items_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_search ON public.media_item_profiles USING gin (search_vector);


--
-- Name: idx_media_items_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_items_slug ON public.media_items USING btree (slug) WHERE (slug <> ''::text);


--
-- Name: idx_media_items_title; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_title ON public.media_item_profiles USING btree (title);


--
-- Name: idx_media_items_title_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_title_trgm ON public.media_item_profiles USING gin (lower(title) public.gin_trgm_ops);


--
-- Name: idx_media_items_tmdb_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_items_tmdb_unique ON public.media_item_external_ids USING btree (library_id, external_id) WHERE (provider = 'tmdb'::text);


--
-- Name: idx_media_items_tvdb_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_items_tvdb_unique ON public.media_item_external_ids USING btree (library_id, external_id) WHERE (provider = 'tvdb'::text);


--
-- Name: idx_media_items_type_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_items_type_created ON public.media_items USING btree (media_type, created_at DESC, id DESC);


--
-- Name: idx_media_overviews_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_overviews_media ON public.media_overviews USING btree (media_item_id);


--
-- Name: idx_media_rec_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_rec_media ON public.media_recommendations USING btree (media_item_id);


--
-- Name: idx_media_segments_file; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_segments_file ON public.media_segments USING btree (library_file_id);


--
-- Name: idx_media_segments_file_type; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_media_segments_file_type ON public.media_segments USING btree (library_file_id, segment_type) WHERE (segment_type <> 'commercial'::text);


--
-- Name: idx_media_titles_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_titles_media ON public.media_titles USING btree (media_item_id);


--
-- Name: idx_media_videos_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_videos_media ON public.media_videos USING btree (media_item_id);


--
-- Name: idx_metadata_match_candidates_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_metadata_match_candidates_identity ON public.metadata_match_candidates USING btree (identity_id, rank);


--
-- Name: idx_metadata_match_candidates_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_metadata_match_candidates_provider ON public.metadata_match_candidates USING btree (provider_kind, provider_id);


--
-- Name: idx_metadata_match_candidates_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_metadata_match_candidates_status ON public.metadata_match_candidates USING btree (status);


--
-- Name: idx_networks_external_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_networks_external_ids ON public.networks USING gin (external_ids);


--
-- Name: idx_people_external_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_people_external_ids ON public.people USING gin (external_ids);


--
-- Name: idx_people_heya_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_people_heya_slug ON public.people USING btree (heya_slug) WHERE (heya_slug <> ''::text);


--
-- Name: idx_people_lower_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_people_lower_name ON public.people USING btree (lower(name) text_pattern_ops);


--
-- Name: idx_people_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_people_name ON public.people USING btree (name);


--
-- Name: idx_people_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_people_name_trgm ON public.people USING gin (lower(name) public.gin_trgm_ops);


--
-- Name: idx_people_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_people_search ON public.people USING gin (search_vector);


--
-- Name: idx_people_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_people_slug ON public.people USING btree (slug) WHERE (slug <> ''::text);


--
-- Name: idx_person_bios_person; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_person_bios_person ON public.person_biographies USING btree (person_id);


--
-- Name: idx_person_external_credits_external_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_person_external_credits_external_ids ON public.person_external_credits USING gin (external_ids);


--
-- Name: idx_person_external_credits_person; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_person_external_credits_person ON public.person_external_credits USING btree (person_id, kind, display_order);


--
-- Name: idx_person_profiles_person; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_person_profiles_person ON public.person_profiles USING btree (person_id);


--
-- Name: idx_scan_findings_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scan_findings_code ON public.scan_findings USING btree (library_id, code) WHERE (resolved_at IS NULL);


--
-- Name: idx_scan_findings_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scan_findings_identity ON public.scan_findings USING btree (identity_id) WHERE (identity_id IS NOT NULL);


--
-- Name: idx_scan_findings_library; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scan_findings_library ON public.scan_findings USING btree (library_id, created_at DESC);


--
-- Name: idx_scan_runs_library_started; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scan_runs_library_started ON public.scan_runs USING btree (library_id, started_at DESC);


--
-- Name: idx_scan_runs_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scan_runs_status ON public.scan_runs USING btree (status);


--
-- Name: idx_sessions_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_expires_at ON public.sessions USING btree (expires_at);


--
-- Name: idx_sessions_token_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_token_hash ON public.sessions USING btree (token_hash);


--
-- Name: idx_sessions_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user_id ON public.sessions USING btree (user_id);


--
-- Name: idx_sessions_user_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user_kind ON public.sessions USING btree (user_id, kind);


--
-- Name: idx_track_files_library_file; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_track_files_library_file ON public.track_files USING btree (library_file_id);


--
-- Name: idx_track_files_quality; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_track_files_quality ON public.track_files USING btree (track_id, quality_score DESC);


--
-- Name: idx_track_files_track; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_track_files_track ON public.track_files USING btree (track_id);


--
-- Name: idx_tracks_external_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tracks_external_ids ON public.tracks USING gin (external_ids);


--
-- Name: idx_tracks_isrc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tracks_isrc ON public.tracks USING btree (isrc) WHERE (isrc <> ''::text);


--
-- Name: idx_tracks_library_file_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tracks_library_file_id ON public.tracks USING btree (library_file_id) WHERE (library_file_id IS NOT NULL);


--
-- Name: idx_tracks_recording_mbid; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tracks_recording_mbid ON public.tracks USING btree (recording_mbid) WHERE (recording_mbid <> ''::text);


--
-- Name: idx_tracks_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tracks_search ON public.tracks USING gin (search_vector);


--
-- Name: idx_tracks_title_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tracks_title_trgm ON public.tracks USING gin (lower(title) public.gin_trgm_ops);


--
-- Name: idx_user_favorites_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_favorites_entity ON public.user_favorites USING btree (entity_type, entity_id);


--
-- Name: idx_user_favorites_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_favorites_user ON public.user_favorites USING btree (user_id);


--
-- Name: idx_user_list_items_list; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_list_items_list ON public.user_list_items USING btree (list_id);


--
-- Name: idx_user_playback_prefs_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_playback_prefs_user ON public.user_playback_preferences USING btree (user_id);


--
-- Name: idx_user_playlist_tracks_playlist_position; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_playlist_tracks_playlist_position ON public.user_playlist_tracks USING btree (playlist_id, "position");


--
-- Name: idx_user_playlists_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_playlists_user ON public.user_playlists USING btree (user_id);


--
-- Name: idx_users_username; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_username ON public.users USING btree (username);


--
-- Name: idx_uwp_continue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_uwp_continue ON public.user_watch_progress USING btree (user_id, completed, updated_at DESC) WHERE ((completed = false) AND (progress_seconds > 0));


--
-- Name: idx_uwp_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_uwp_entity ON public.user_watch_progress USING btree (entity_type, entity_id);


--
-- Name: idx_uwp_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_uwp_user ON public.user_watch_progress USING btree (user_id);


--
-- Name: play_events_track_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX play_events_track_idx ON public.play_events USING btree (track_id);


--
-- Name: play_events_user_played_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX play_events_user_played_idx ON public.play_events USING btree (user_id, played_at DESC);


--
-- Name: track_facets_text_emb_hnsw; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX track_facets_text_emb_hnsw ON public.track_facets USING hnsw (text_embedding vector_cosine_ops);


--
-- Name: track_facets_track_emb_hnsw; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX track_facets_track_emb_hnsw ON public.track_facets USING hnsw (track_embedding vector_cosine_ops);


--
-- Name: track_facets_version_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX track_facets_version_idx ON public.track_facets USING btree (analyzer_version);


--
-- Name: uq_albums_artist_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_albums_artist_slug ON public.albums USING btree (artist_id, slug) WHERE (slug <> ''::text);


--
-- Name: uq_albums_artist_title_year; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_albums_artist_title_year ON public.albums USING btree (artist_id, lower(title), year);


--
-- Name: uq_artists_name_disambig; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_artists_name_disambig ON public.artists USING btree (lower(name), lower(disambiguation)) WHERE (name <> ''::text);


--
-- Name: user_album_ratings_user_rating_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_album_ratings_user_rating_idx ON public.user_album_ratings USING btree (user_id, rating DESC, updated_at DESC);


--
-- Name: user_artist_ratings_user_rating_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_artist_ratings_user_rating_idx ON public.user_artist_ratings USING btree (user_id, rating DESC, updated_at DESC);


--
-- Name: user_podcast_progress_user_updated_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_podcast_progress_user_updated_idx ON public.user_podcast_progress USING btree (user_id, updated_at DESC) WHERE ((completed = false) AND (progress_seconds > 0));


--
-- Name: user_podcast_subscriptions_user_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_podcast_subscriptions_user_idx ON public.user_podcast_subscriptions USING btree (user_id, created_at DESC);


--
-- Name: user_radio_favorites_user_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_radio_favorites_user_idx ON public.user_radio_favorites USING btree (user_id, created_at DESC);


--
-- Name: user_radio_recents_user_played_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_radio_recents_user_played_idx ON public.user_radio_recents USING btree (user_id, played_at DESC);


--
-- Name: user_track_ratings_user_rating_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_track_ratings_user_rating_idx ON public.user_track_ratings USING btree (user_id, rating DESC, updated_at DESC);


--
-- Name: album_centroids album_centroids_album_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.album_centroids
    ADD CONSTRAINT album_centroids_album_id_fkey FOREIGN KEY (album_id) REFERENCES public.albums(id) ON DELETE CASCADE;


--
-- Name: albums albums_artist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.albums
    ADD CONSTRAINT albums_artist_id_fkey FOREIGN KEY (artist_id) REFERENCES public.artists(id) ON DELETE CASCADE;


--
-- Name: artist_centroids artist_centroids_artist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artist_centroids
    ADD CONSTRAINT artist_centroids_artist_id_fkey FOREIGN KEY (artist_id) REFERENCES public.artists(id) ON DELETE CASCADE;


--
-- Name: artist_similar_artists artist_similar_artists_artist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artist_similar_artists
    ADD CONSTRAINT artist_similar_artists_artist_id_fkey FOREIGN KEY (artist_id) REFERENCES public.artists(id) ON DELETE CASCADE;


--
-- Name: artist_similar_artists artist_similar_artists_local_artist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artist_similar_artists
    ADD CONSTRAINT artist_similar_artists_local_artist_id_fkey FOREIGN KEY (local_artist_id) REFERENCES public.artists(id) ON DELETE SET NULL;


--
-- Name: artist_top_tracks artist_top_tracks_artist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artist_top_tracks
    ADD CONSTRAINT artist_top_tracks_artist_id_fkey FOREIGN KEY (artist_id) REFERENCES public.artists(id) ON DELETE CASCADE;


--
-- Name: artists artists_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artists
    ADD CONSTRAINT artists_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: books books_author_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.books
    ADD CONSTRAINT books_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.authors(id) ON DELETE SET NULL;


--
-- Name: books books_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.books
    ADD CONSTRAINT books_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: debounced_enriches debounced_enriches_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.debounced_enriches
    ADD CONSTRAINT debounced_enriches_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: episode_overviews episode_overviews_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_overviews
    ADD CONSTRAINT episode_overviews_episode_id_fkey FOREIGN KEY (episode_id) REFERENCES public.tv_episodes(id) ON DELETE CASCADE;


--
-- Name: episode_titles episode_titles_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_titles
    ADD CONSTRAINT episode_titles_episode_id_fkey FOREIGN KEY (episode_id) REFERENCES public.tv_episodes(id) ON DELETE CASCADE;


--
-- Name: external_ratings external_ratings_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_ratings
    ADD CONSTRAINT external_ratings_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: libraries libraries_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.libraries
    ADD CONSTRAINT libraries_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: library_disk_usage library_disk_usage_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_disk_usage
    ADD CONSTRAINT library_disk_usage_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;


--
-- Name: library_file_links library_file_links_identity_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_file_links
    ADD CONSTRAINT library_file_links_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES public.local_media_identities(id) ON DELETE SET NULL;


--
-- Name: library_file_links library_file_links_library_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_file_links
    ADD CONSTRAINT library_file_links_library_file_id_fkey FOREIGN KEY (library_file_id) REFERENCES public.library_files(id) ON DELETE CASCADE;


--
-- Name: library_file_links library_file_links_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_file_links
    ADD CONSTRAINT library_file_links_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: library_file_links library_file_links_movie_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_file_links
    ADD CONSTRAINT library_file_links_movie_id_fkey FOREIGN KEY (movie_id) REFERENCES public.movies(id) ON DELETE CASCADE;


--
-- Name: library_file_links library_file_links_scan_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_file_links
    ADD CONSTRAINT library_file_links_scan_run_id_fkey FOREIGN KEY (scan_run_id) REFERENCES public.scan_runs(id) ON DELETE SET NULL;


--
-- Name: library_file_links library_file_links_tv_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_file_links
    ADD CONSTRAINT library_file_links_tv_episode_id_fkey FOREIGN KEY (tv_episode_id) REFERENCES public.tv_episodes(id) ON DELETE CASCADE;


--
-- Name: library_files library_files_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_files
    ADD CONSTRAINT library_files_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;


--
-- Name: library_files library_files_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_files
    ADD CONSTRAINT library_files_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE SET NULL;


--
-- Name: library_nfo_dirs library_nfo_dirs_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.library_nfo_dirs
    ADD CONSTRAINT library_nfo_dirs_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;


--
-- Name: local_media_identities local_media_identities_first_seen_scan_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.local_media_identities
    ADD CONSTRAINT local_media_identities_first_seen_scan_run_id_fkey FOREIGN KEY (first_seen_scan_run_id) REFERENCES public.scan_runs(id) ON DELETE SET NULL;


--
-- Name: local_media_identities local_media_identities_last_seen_scan_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.local_media_identities
    ADD CONSTRAINT local_media_identities_last_seen_scan_run_id_fkey FOREIGN KEY (last_seen_scan_run_id) REFERENCES public.scan_runs(id) ON DELETE SET NULL;


--
-- Name: local_media_identities local_media_identities_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.local_media_identities
    ADD CONSTRAINT local_media_identities_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;


--
-- Name: local_media_identities local_media_identities_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.local_media_identities
    ADD CONSTRAINT local_media_identities_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE SET NULL;


--
-- Name: local_media_identity_external_ids local_media_identity_external_ids_identity_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.local_media_identity_external_ids
    ADD CONSTRAINT local_media_identity_external_ids_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES public.local_media_identities(id) ON DELETE CASCADE;


--
-- Name: match_candidates match_candidates_library_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.match_candidates
    ADD CONSTRAINT match_candidates_library_file_id_fkey FOREIGN KEY (library_file_id) REFERENCES public.library_files(id) ON DELETE CASCADE;


--
-- Name: media_assets media_assets_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_assets
    ADD CONSTRAINT media_assets_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_cast media_cast_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_cast
    ADD CONSTRAINT media_cast_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_cast media_cast_person_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_cast
    ADD CONSTRAINT media_cast_person_id_fkey FOREIGN KEY (person_id) REFERENCES public.people(id) ON DELETE CASCADE;


--
-- Name: media_certifications media_certifications_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_certifications
    ADD CONSTRAINT media_certifications_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_crew media_crew_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_crew
    ADD CONSTRAINT media_crew_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_crew media_crew_person_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_crew
    ADD CONSTRAINT media_crew_person_id_fkey FOREIGN KEY (person_id) REFERENCES public.people(id) ON DELETE CASCADE;


--
-- Name: media_item_external_ids media_item_external_ids_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_item_external_ids
    ADD CONSTRAINT media_item_external_ids_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_item_external_ids media_item_external_ids_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_item_external_ids
    ADD CONSTRAINT media_item_external_ids_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;

--
-- Name: media_item_profiles media_item_profiles_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_item_profiles
    ADD CONSTRAINT media_item_profiles_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_items media_items_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_items
    ADD CONSTRAINT media_items_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;


--
-- Name: media_keywords media_keywords_keyword_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_keywords
    ADD CONSTRAINT media_keywords_keyword_id_fkey FOREIGN KEY (keyword_id) REFERENCES public.keywords(id) ON DELETE CASCADE;


--
-- Name: media_keywords media_keywords_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_keywords
    ADD CONSTRAINT media_keywords_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_overviews media_overviews_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_overviews
    ADD CONSTRAINT media_overviews_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_production_companies media_production_companies_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_production_companies
    ADD CONSTRAINT media_production_companies_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.production_companies(id) ON DELETE CASCADE;


--
-- Name: media_production_companies media_production_companies_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_production_companies
    ADD CONSTRAINT media_production_companies_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_recommendations media_recommendations_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_recommendations
    ADD CONSTRAINT media_recommendations_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_segments media_segments_library_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_segments
    ADD CONSTRAINT media_segments_library_file_id_fkey FOREIGN KEY (library_file_id) REFERENCES public.library_files(id) ON DELETE CASCADE;


--
-- Name: media_titles media_titles_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_titles
    ADD CONSTRAINT media_titles_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: media_videos media_videos_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_videos
    ADD CONSTRAINT media_videos_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: metadata_match_candidates metadata_match_candidates_identity_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metadata_match_candidates
    ADD CONSTRAINT metadata_match_candidates_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES public.local_media_identities(id) ON DELETE CASCADE;


--
-- Name: metadata_match_candidates metadata_match_candidates_scan_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.metadata_match_candidates
    ADD CONSTRAINT metadata_match_candidates_scan_run_id_fkey FOREIGN KEY (scan_run_id) REFERENCES public.scan_runs(id) ON DELETE SET NULL;


--
-- Name: movies movies_collection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.movies
    ADD CONSTRAINT movies_collection_id_fkey FOREIGN KEY (collection_id) REFERENCES public.collections(id);


--
-- Name: movies movies_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.movies
    ADD CONSTRAINT movies_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: person_biographies person_biographies_person_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.person_biographies
    ADD CONSTRAINT person_biographies_person_id_fkey FOREIGN KEY (person_id) REFERENCES public.people(id) ON DELETE CASCADE;


--
-- Name: person_external_credits person_external_credits_person_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.person_external_credits
    ADD CONSTRAINT person_external_credits_person_id_fkey FOREIGN KEY (person_id) REFERENCES public.people(id) ON DELETE CASCADE;


--
-- Name: person_profiles person_profiles_person_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.person_profiles
    ADD CONSTRAINT person_profiles_person_id_fkey FOREIGN KEY (person_id) REFERENCES public.people(id) ON DELETE CASCADE;


--
-- Name: play_events play_events_track_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.play_events
    ADD CONSTRAINT play_events_track_id_fkey FOREIGN KEY (track_id) REFERENCES public.tracks(id) ON DELETE CASCADE;


--
-- Name: play_events play_events_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.play_events
    ADD CONSTRAINT play_events_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: scan_findings scan_findings_identity_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scan_findings
    ADD CONSTRAINT scan_findings_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES public.local_media_identities(id) ON DELETE SET NULL;


--
-- Name: scan_findings scan_findings_library_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scan_findings
    ADD CONSTRAINT scan_findings_library_file_id_fkey FOREIGN KEY (library_file_id) REFERENCES public.library_files(id) ON DELETE SET NULL;


--
-- Name: scan_findings scan_findings_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scan_findings
    ADD CONSTRAINT scan_findings_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;


--
-- Name: scan_findings scan_findings_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scan_findings
    ADD CONSTRAINT scan_findings_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE SET NULL;


--
-- Name: scan_findings scan_findings_scan_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scan_findings
    ADD CONSTRAINT scan_findings_scan_run_id_fkey FOREIGN KEY (scan_run_id) REFERENCES public.scan_runs(id) ON DELETE SET NULL;


--
-- Name: scan_runs scan_runs_library_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scan_runs
    ADD CONSTRAINT scan_runs_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE;


--
-- Name: sessions sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: track_facets track_facets_track_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.track_facets
    ADD CONSTRAINT track_facets_track_id_fkey FOREIGN KEY (track_id) REFERENCES public.tracks(id) ON DELETE CASCADE;


--
-- Name: track_files track_files_library_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.track_files
    ADD CONSTRAINT track_files_library_file_id_fkey FOREIGN KEY (library_file_id) REFERENCES public.library_files(id) ON DELETE CASCADE;


--
-- Name: track_files track_files_track_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.track_files
    ADD CONSTRAINT track_files_track_id_fkey FOREIGN KEY (track_id) REFERENCES public.tracks(id) ON DELETE CASCADE;


--
-- Name: tracks tracks_album_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tracks
    ADD CONSTRAINT tracks_album_id_fkey FOREIGN KEY (album_id) REFERENCES public.albums(id) ON DELETE CASCADE;


--
-- Name: tracks tracks_library_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tracks
    ADD CONSTRAINT tracks_library_file_id_fkey FOREIGN KEY (library_file_id) REFERENCES public.library_files(id) ON DELETE CASCADE;


--
-- Name: tv_episodes tv_episodes_season_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tv_episodes
    ADD CONSTRAINT tv_episodes_season_id_fkey FOREIGN KEY (season_id) REFERENCES public.tv_seasons(id) ON DELETE CASCADE;


--
-- Name: tv_seasons tv_seasons_series_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tv_seasons
    ADD CONSTRAINT tv_seasons_series_id_fkey FOREIGN KEY (series_id) REFERENCES public.tv_series(id) ON DELETE CASCADE;


--
-- Name: tv_series_creators tv_series_creators_creator_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tv_series_creators
    ADD CONSTRAINT tv_series_creators_creator_id_fkey FOREIGN KEY (creator_id) REFERENCES public.creators(id) ON DELETE CASCADE;


--
-- Name: tv_series_creators tv_series_creators_series_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tv_series_creators
    ADD CONSTRAINT tv_series_creators_series_id_fkey FOREIGN KEY (series_id) REFERENCES public.tv_series(id) ON DELETE CASCADE;


--
-- Name: tv_series tv_series_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tv_series
    ADD CONSTRAINT tv_series_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: tv_series_networks tv_series_networks_network_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tv_series_networks
    ADD CONSTRAINT tv_series_networks_network_id_fkey FOREIGN KEY (network_id) REFERENCES public.networks(id) ON DELETE CASCADE;


--
-- Name: tv_series_networks tv_series_networks_series_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tv_series_networks
    ADD CONSTRAINT tv_series_networks_series_id_fkey FOREIGN KEY (series_id) REFERENCES public.tv_series(id) ON DELETE CASCADE;


--
-- Name: user_album_ratings user_album_ratings_album_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_album_ratings
    ADD CONSTRAINT user_album_ratings_album_id_fkey FOREIGN KEY (album_id) REFERENCES public.albums(id) ON DELETE CASCADE;


--
-- Name: user_album_ratings user_album_ratings_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_album_ratings
    ADD CONSTRAINT user_album_ratings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_artist_ratings user_artist_ratings_artist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_artist_ratings
    ADD CONSTRAINT user_artist_ratings_artist_id_fkey FOREIGN KEY (artist_id) REFERENCES public.artists(id) ON DELETE CASCADE;


--
-- Name: user_artist_ratings user_artist_ratings_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_artist_ratings
    ADD CONSTRAINT user_artist_ratings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_favorites user_favorites_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_favorites
    ADD CONSTRAINT user_favorites_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_list_items user_list_items_list_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_list_items
    ADD CONSTRAINT user_list_items_list_id_fkey FOREIGN KEY (list_id) REFERENCES public.user_lists(id) ON DELETE CASCADE;


--
-- Name: user_list_items user_list_items_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_list_items
    ADD CONSTRAINT user_list_items_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: user_lists user_lists_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_lists
    ADD CONSTRAINT user_lists_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_playback_preferences user_playback_preferences_media_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_playback_preferences
    ADD CONSTRAINT user_playback_preferences_media_item_id_fkey FOREIGN KEY (media_item_id) REFERENCES public.media_items(id) ON DELETE CASCADE;


--
-- Name: user_playback_preferences user_playback_preferences_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_playback_preferences
    ADD CONSTRAINT user_playback_preferences_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_playlist_tracks user_playlist_tracks_playlist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_playlist_tracks
    ADD CONSTRAINT user_playlist_tracks_playlist_id_fkey FOREIGN KEY (playlist_id) REFERENCES public.user_playlists(id) ON DELETE CASCADE;


--
-- Name: user_playlist_tracks user_playlist_tracks_track_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_playlist_tracks
    ADD CONSTRAINT user_playlist_tracks_track_id_fkey FOREIGN KEY (track_id) REFERENCES public.tracks(id) ON DELETE CASCADE;


--
-- Name: user_playlists user_playlists_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_playlists
    ADD CONSTRAINT user_playlists_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_podcast_progress user_podcast_progress_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_podcast_progress
    ADD CONSTRAINT user_podcast_progress_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_podcast_subscriptions user_podcast_subscriptions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_podcast_subscriptions
    ADD CONSTRAINT user_podcast_subscriptions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_radio_favorites user_radio_favorites_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_radio_favorites
    ADD CONSTRAINT user_radio_favorites_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_radio_recents user_radio_recents_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_radio_recents
    ADD CONSTRAINT user_radio_recents_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_track_ratings user_track_ratings_track_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_track_ratings
    ADD CONSTRAINT user_track_ratings_track_id_fkey FOREIGN KEY (track_id) REFERENCES public.tracks(id) ON DELETE CASCADE;


--
-- Name: user_track_ratings user_track_ratings_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_track_ratings
    ADD CONSTRAINT user_track_ratings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_watch_progress user_watch_progress_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_watch_progress
    ADD CONSTRAINT user_watch_progress_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--



-- Seed canonical scheduled task definitions. Runtime state stays at defaults.
INSERT INTO public.scheduled_tasks (id, display_name, description, category, enabled, interval_hours, daily_start_time, daily_end_time, max_runtime_minutes) VALUES
('generate_trickplay', 'Generate Trickplay Sprites', 'Create timeline preview thumbnails for video files in libraries with trickplay enabled', 'media', false, 24, '02:00', '06:00', 120),
('generate_thumbnails', 'Generate Missing Thumbnails', 'Extract thumbnail frames for extras and episodes without artwork', 'media', false, 24, '02:00', '06:00', 120),
('analyze_music_facets', 'Analyze Music (Sonic)', 'Per-track ML/DSP analysis: embeddings, BPM, key, loudness, mood, waveform. Runs one track at a time during the configured off-hours window.', 'media', false, 24, '02:00', '06:00', 240),
('detect_media_segments', 'Detect Skip Segments', 'Local chromaprint cross-episode intro/credits detection for TV and ffmpeg black-frame credits detection for movies, for files the community skip-segment databases could not resolve', 'library', true, 24, '02:00', '06:00', 120),
('scan_media_segments', 'Fetch Skip Segments', 'Community intro/credits skip markers from heya.media for movie and episode files', 'library', true, 24, '02:00', '06:00', 120),
('refresh_stale_items', 'Refresh Stale Metadata', 'Re-fetch metadata from heya.media for any media item past its library''s MetadataRefreshDays staleness window. Covers movies, TV, music, and books.', 'library', true, 24, '02:00', '06:00', 120),
('scan_libraries', 'Scan Libraries', 'Scan all library paths for new, changed, or deleted media files', 'library', true, 24, '02:00', '06:00', 120),
('scan_music_fingerprint', 'Scan Music Fingerprints', 'Chromaprint audio fingerprints for music files - powers duplicate-recording detection and future fingerprint submission', 'library', true, 24, '02:00', '06:00', 120),
('scan_music_loudness', 'Scan Music Loudness', 'Backstop for the ebur128 pipeline - enqueues track and album loudness analysis for any music files not yet measured', 'library', true, 24, '02:00', '06:00', 120);

-- +goose Down
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;
