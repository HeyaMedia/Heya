export interface TrackAlbumInfo {
  trackId?: string | number
  albumId?: string | number
  albumName?: string
  discNumber?: number
  trackNumber?: number
}

function sameAlbum(trackA: TrackAlbumInfo, trackB: TrackAlbumInfo): boolean {
  if (trackA.albumId != null && trackB.albumId != null) return trackA.albumId === trackB.albumId
  if (trackA.albumName && trackB.albumName) {
    return trackA.albumName.trim().toLocaleLowerCase() === trackB.albumName.trim().toLocaleLowerCase()
  }
  return false
}

// Album order is an invariant, not a preference: adjacent tracks and repeat-one
// loops must meet without an overlap. If old/local queue data lacks sequence
// metadata, same-release identity is the conservative fallback.
export function shouldSuppressCrossfade(trackA: TrackAlbumInfo, trackB: TrackAlbumInfo): boolean {
  if (trackA.trackId != null && trackA.trackId === trackB.trackId) return true
  if (!sameAlbum(trackA, trackB)) return false

  const aTrack = trackA.trackNumber
  const bTrack = trackB.trackNumber
  if (!aTrack || !bTrack) return true
  const aDisc = trackA.discNumber || 1
  const bDisc = trackB.discNumber || 1
  return (aDisc === bDisc && bTrack === aTrack + 1)
    || (bDisc === aDisc + 1 && bTrack === 1)
}
