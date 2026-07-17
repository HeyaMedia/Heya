# Playlist synchronization

Playlist synchronization is opt-in. A link exists in `user_playlist_syncs`
only while the user has enabled synchronization for that local/provider pair.
Disabling sync or deleting a local playlist does not delete the provider copy.

## Architecture

`internal/playlistsync.Provider` is the provider boundary. It exposes:

- capability discovery;
- list/get playlist reads;
- create and full replacement writes;
- normalized provider track IDs.

The service layer owns credentials, local track matching, persistence,
conflict resolution, scheduling, and HTTP views. Provider adapters do not
access the database.

Each link stores the last common ordered track-ID sequence as a three-way
merge base. Deletions on either side remove an item from that base, additions
on both sides survive, and a one-sided reorder is preserved. Provider tracks
that cannot yet be matched locally are tracked separately so they remain on
the provider and can appear locally after the library gains a match.

Local mutations trigger an immediate background pass. A five-minute poll
imports provider-side changes; users can also request a pass from either UI.

Provider-owned collections use `pull_only` links. ListenBrainz “Created for
You” playlists are listed separately in settings: users can import selected
playlists or enable a collection policy which discovers and imports future
generated playlists automatically. Their track sequence always flows from
ListenBrainz to Heya and is never written back.

Recurring generated series (Weekly Jams, Weekly Exploration, Daily Jams —
identified by the JSPF `source_patch` field) behave like Spotify’s Discover
Weekly: Heya keeps a single local playlist per series, named after the series
(“Weekly Jams”, without the per-edition “for user, week of …” decoration).
Each new edition is a brand-new ListenBrainz playlist; the reconciler
re-points the existing link (`user_playlist_syncs.series`) at the newest
edition and refills the same local playlist instead of importing another
copy. Per-edition mirrors imported before series tracking are claimed (newest
becomes the series playlist) and the rest are deleted. One-off generated
playlists (yearly discoveries, missed tracks) still import individually.

## Adding a provider

1. Implement `playlistsync.Provider`, select its local identity kind
   (`recording_mbid`, `isrc`, or provider service ID), and test its wire format.
2. Register its credential-aware constructor in `playlistSyncProvider` and
   advertise its real capabilities in `playlistSyncCapabilities`.
3. Add the service to credential validation and the API path enums.
4. Add its connection controls and catalog card to Music services settings.

Provider IDs do not have to be MusicBrainz IDs. ListenBrainz uses recording
MBIDs; a Spotify/Tidal/Qobuz adapter can use its own stable track IDs and add a
provider-specific local resolver without changing the merge or link model.

## Current providers

- ListenBrainz: read/write two-way synchronization through its JSPF API.
- Last.fm: unavailable. Last.fm marks its playlist API deprecated and no
  longer supported: <https://www.last.fm/api/playlists>.
