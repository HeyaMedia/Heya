// Strip TMDB's trailing " Collection" from a franchise/collection name for
// display — "The Terminator Collection" → "The Terminator". Falls back to the
// original if stripping would leave nothing (e.g. a franchise literally named
// "Collection").
export function franchiseLabel(name: string): string {
  return name.replace(/\s*Collection$/i, '').trim() || name
}
