import type { RecordingCredit } from '~~/shared/types'

// Shared humanize + group-by-role logic for performance credits (album page
// Credits section + the per-track Track-info dialog). Roles/attributes come
// off the wire snake_case ("executive_producer", "electric_guitar").

export interface CreditGroup {
  role: string
  names: string[]
}

/** "electric_guitar" → "Electric guitar" — snake_case to spaces, sentence case. */
export function humanizeCreditTerm(value: string): string {
  const spaced = value.replace(/_/g, ' ').trim()
  if (!spaced) return ''
  return spaced.charAt(0).toUpperCase() + spaced.slice(1).toLowerCase()
}

// Attributes are the specific fact ("instrument" + "electric_guitar" reads
// far better as "Electric guitar" than the generic "Instrument"); roles
// without attributes (producer, engineer, mix, ...) use the role itself.
function creditGroupLabel(credit: RecordingCredit): string {
  if (credit.attributes?.length) {
    return credit.attributes.map(humanizeCreditTerm).filter(Boolean).join(', ')
  }
  return humanizeCreditTerm(credit.role)
}

/** Groups credits by humanized role/attribute label, deduping artist names
 *  within each group (first-seen casing), sorted alphabetically by label. */
export function groupTrackCredits(credits: RecordingCredit[]): CreditGroup[] {
  const byLabel = new Map<string, Set<string>>()
  for (const c of credits) {
    const label = creditGroupLabel(c)
    const name = c.artist_name?.trim()
    if (!label || !name) continue
    if (!byLabel.has(label)) byLabel.set(label, new Set())
    byLabel.get(label)!.add(name)
  }
  return [...byLabel.entries()]
    .map(([role, names]) => ({ role, names: [...names] }))
    .sort((a, b) => a.role.localeCompare(b.role))
}
