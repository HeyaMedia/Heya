export interface TrackAlbumInfo {
  albumId?: string | number
  albumName?: string
}

// Inside an album, tracks are mastered to flow into each other — crossfading
// breaks that. Suppress whenever both tracks come from the same release.
export function shouldSuppressCrossfade(trackA: TrackAlbumInfo, trackB: TrackAlbumInfo): boolean {
  if (trackA.albumId != null && trackB.albumId != null) {
    return trackA.albumId === trackB.albumId
  }
  if (trackA.albumName && trackB.albumName) {
    return trackA.albumName.toLowerCase() === trackB.albumName.toLowerCase()
  }
  return false
}
