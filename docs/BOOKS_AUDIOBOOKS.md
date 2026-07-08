# Books and audiobooks metadata API

Status: 2026-07-08. Heya should use the external heya.media metadata API for book and audiobook matching. Do not read Calibre `metadata.opf` or other local sidecar metadata for canonical metadata.

## Metadata-server contract

Base URL is whatever the Heya deployment config uses for heya.media / metadata-server. Local development is usually:

```text
http://localhost:3030
```

All endpoints below are read-only from Heya's perspective. Fetch endpoints enrich inline on a cold miss and then return the cached document on later calls.

## Search a scanned book or audiobook

Use book search with a parsed title, author, optional year, and optional format hint:

```http
GET /api/v1/search?type=book&q={title}&author={author}&year={year}&format={format}
```

Parameters:

| Name | Required | Notes |
| --- | --- | --- |
| `type` | yes | Always `book` for books and audiobooks. |
| `q` | yes | Parsed title, without author/year/path noise when possible. |
| `author` | strongly recommended | Enables author-first OpenLibrary matching. If omitted, metadata-server falls back to direct OpenLibrary title search. |
| `year` | optional | First-publish year from filename/folder if available. Helps disambiguate. |
| `format` | optional | `book` or `audiobook`. Use `audiobook` for audiobook libraries so Audible evidence participates in weak-match promotion. |
| `limit` | optional | Max upstream search results. Default is fine; max is `50`. |

Examples:

```http
GET /api/v1/search?type=book&q=Project%20Hail%20Mary&author=Andy%20Weir&year=2021
GET /api/v1/search?type=book&q=The%20Martian&author=Andy%20Weir&format=audiobook
```

Expected response shape:

```json
{
  "type": "book",
  "query": "Project Hail Mary",
  "results": [
    {
      "id": "ol_work_id:OL21745884W",
      "kind": "book",
      "name": "Project Hail Mary",
      "year": 2021,
      "image": "https://covers.openlibrary.org/b/id/...-L.jpg",
      "snippet": "Andy Weir",
      "sources": ["openlib"],
      "external_ids": { "ol_work_id": "OL21745884W" },
      "score": 0.7,
      "enriched": false
    }
  ]
}
```

Importer rule:

- Treat the first result as the canonical match when `score >= 0.7`.
- Store `external_ids.ol_work_id` as the canonical upstream identity.
- Then fetch `/api/v1/book/{id}` using the returned `id` field.
- If there are no results, leave the local item visible but pending metadata; retry later or expose it for manual matching.

## Fetch/enrich a book document

Round-trip the search result `id` directly:

```http
GET /api/v1/book/ol_work_id:OL21745884W
```

The book endpoint uses OpenLibrary as the canonical work, then supplements with Google Books and Audible when those matches are confident.

Important top-level fields:

| Field | Notes |
| --- | --- |
| `id` | HeyaMedia document id. |
| `kind` | `book`. |
| `title` | Canonical title. |
| `year` | First publish year when known. |
| `slug` | Stable metadata-server slug. Heya may keep its own local slug if needed. |
| `poster` | Best cover URL. |
| `ids.ol_work_id` | Canonical OpenLibrary work id. |
| `payload` | Rich book payload. |
| `payload.authors` | Author/contributor refs. |
| `payload.edition` | Google Books edition data when matched. |
| `payload.audiobook` | Audible-specific data when matched. |
| `providers_ok` / `providers_failed` | Upstream provenance. |

## Author search and author documents

Author search mirrors the music artist flow: ask for an author and get an author entity back.

```http
GET /api/v1/search?type=author&q=Andy%20Weir
GET /api/v1/author/ol_author_id:OL68149A
```

Author documents include the OpenLibrary profile and up to 1000 works in `payload.works`. This is useful for author pages and for manual matching UI, but import matching should still use the book search endpoint above because it applies title/year evidence and Google/Audible fallback promotion.

## Recommended Heya import flow

1. Parse the filesystem path into `{format, title, author, year}`.
2. Call `/api/v1/search?type=book&q=...&author=...&year=...&format=...`.
3. If the best result is acceptable, store the `ol_work_id` on the local media item.
4. Call `/api/v1/book/{id}` to fetch the rich metadata document.
5. Map top-level metadata into Heya's local `media_items` fields and store the raw payload/provenance where Heya normally stores enriched metadata.
6. If enrichment fails with 503 or 429 semantics, keep the item pending and retry. Do not mark it as a permanent miss.
7. If fetch returns 404 for the upstream id, treat that specific id as bad and re-run search/manual matching.

## Matching notes

- Author-first matching asks OpenLibrary for the author, fetches up to 1000 works, and matches title/year locally.
- If author works do not produce a clean match, metadata-server falls back to direct OpenLibrary title+author search.
- Weak OpenLibrary fallback matches can be promoted only when Google Books or Audible provides matching title/author/year evidence.
- For audiobooks, pass `format=audiobook`; the canonical identity is still the OpenLibrary work id, while Audible data is stored as supplemental audiobook metadata.
- Metadata-server does not expose a separate `audiobook` kind today. Heya can model local media type as audiobook while using `kind=book` for metadata lookup.

## Error handling

| Status | Meaning | Heya behavior |
| --- | --- | --- |
| `200` | Found and/or enriched. | Store/update metadata. |
| `400` / `422` | Bad request shape. | Fix parser/caller; do not retry unchanged. |
| `404` | Existing id not found upstream, or slug miss. | Treat as a bad id/manual-match case. |
| `503` | Upstream/cache/enrichment temporarily unavailable or capacity gated. | Retry later. Respect `Retry-After` when present. |

## Quick curl examples

```bash
curl 'http://localhost:3030/api/v1/search?type=book&q=Project%20Hail%20Mary&author=Andy%20Weir&year=2021'
curl 'http://localhost:3030/api/v1/book/ol_work_id:OL21745884W'
curl 'http://localhost:3030/api/v1/search?type=author&q=Andy%20Weir'
curl 'http://localhost:3030/api/v1/author/ol_author_id:OL68149A'
```
