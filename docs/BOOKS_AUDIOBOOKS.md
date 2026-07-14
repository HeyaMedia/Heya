# Books and audiobooks through HeyaMetadata V2

Status: 2026-07-13. Heya uses HeyaMetadata V2 for canonical publication
identity. Calibre `metadata.opf` and other local sidecars may contribute scan
hints, but they do not replace canonical identity.

## Contract and identity

The service base URL is `HEYA_METADATA_URL` (default
`http://localhost:3030`). Heya uses the generated contract under
`clients/heyametadata`; do not add hand-written V1 calls.

Publication kinds remain distinct:

- `book_work` is the intellectual work and is Heya's normal book root;
- `book_edition` is a specific publication;
- `author` has its own canonical UUID;
- manga/comic works, volumes, and editions use their separate V2 kinds.

Heya stores the root UUID in `metadata_entity_bindings` for the media item and
stores the embedded author UUID against the local author row. OpenLibrary,
ISBN, Google Books, and other IDs remain evidence in `external_ids`; they are
not the durable primary key.

## Matching flow

1. Parse `{title, author, year, ISBN, format}` from the scan.
2. Search `GET /api/v2/search?kind=book_work&q=...` for an already-canonical
   entity.
3. On a miss, create a durable `POST /api/v2/discoveries` request with
   `kind=book_work` and structured `hints.authors`, `hints.year`,
   `hints.isbns`, and `hints.type` where available.
4. Poll a `202` discovery resource until it completes.
5. Respect the V2 recommendation. `ambiguous` and `no_match` are never
   automatic. `likely_match` needs multiple corroborating hints.
6. Resolve the selected candidate with `POST /api/v2/resolutions`; poll its
   job when the response is `202`.
7. Fetch the resulting UUID from `GET /api/v2/entities/{id}` and map the
   kind-specific document into Heya's relational read model.

Provider IDs and request-scoped credentials must not be written to workflow
state beyond non-secret resolution evidence. Discovery ID, resolution job ID,
and final entity UUID are durable so a restart resumes rather than forking
work.

## Audiobook safety

V2 publication discovery accepts general title, author, date, ISBN, and format
hints, but the first cutover has no audiobook-specific/Audible identity spine.
Consequently:

- `format=audiobook` is sent as a hint;
- a `likely_match` audiobook remains manual even when ordinary title/author
  similarity is high;
- only V2 `strong_match` may auto-select;
- ambiguous candidates are persisted with recommendation and evidence for
  review.

This is intentionally stricter than the retired V1 behavior.

## Projection mapping

The adapter maps title, description, localized text, subjects, languages,
publish dates, publishers, ISBN-10/13, page count, rating, canonical images,
author identity, and series name/position. Book series membership is currently
stored in Heya's existing `series_name` / `series_number` fields; the embedded
series UUID remains available in the V2 document but Heya has no separate
series table yet.

Opaque image IDs become `GET /api/v2/images/{image-id}` URLs. A first request
may return `202`; the downloader polls the same resource until image bytes are
ready and authenticates requests to the configured trusted metadata origin.

## Refresh and failures

Normal V2 reads are stale-while-revalidate. Heya consumes
`GET /api/v2/changes?after={cursor}&limit=500` and transactionally inserts
refresh jobs before advancing its cursor. It does not run the retired blind
age-based metadata sweep.

- retry transport errors, `408`, `429`, and `5xx` through River;
- treat a terminal `404` for a selected external identity as a re-match case;
- never turn a discovery/resolution failure into a successful local binding;
- leave local files visible while metadata is pending or requires review.

The authoritative migration and acceptance contract remains
`../HeyaMetadata/HEYAMEDIA_V2_MIGRATION.md`.
