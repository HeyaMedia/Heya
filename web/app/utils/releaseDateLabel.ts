// Human-honest release dates for catalog data. MusicBrainz release groups
// often carry only a year; the sync stores that as YYYY-01-01 so the date
// column still sorts, but rendering it verbatim invents day-level precision
// ("Manic — 2020-01-01"). When the stored date is exactly January 1st of the
// row's own year, show just the year — a genuine Jan-1 release degrades to
// its year, which is never wrong, only less specific.
export function releaseDateLabel(date?: string, year?: string): string {
  if (!date) return year ?? ''
  if (year && date === `${year}-01-01`) return year
  return date
}
