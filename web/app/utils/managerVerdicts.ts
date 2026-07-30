// One vocabulary for decision verdicts across the manager: History rows,
// entity decision panels, and the interactive search modal must name the
// same verdict with the same words and the same badge state.
export type ManagerVerdictState = 'ok' | 'warn' | 'error' | 'idle'

export const MANAGER_VERDICT_META: Record<string, { label: string, state: ManagerVerdictState }> = {
  would_grab: { label: 'would grab', state: 'ok' },
  already_satisfied: { label: 'satisfied', state: 'idle' },
  no_acceptable_candidate: { label: 'nothing acceptable', state: 'error' },
  comparison_uncertain: { label: 'uncertain', state: 'warn' },
  configuration_error: { label: 'config error', state: 'error' },
}

export function managerVerdictLabel(verdict: string): string {
  return MANAGER_VERDICT_META[verdict]?.label ?? verdict.replaceAll('_', ' ')
}

export function managerVerdictState(verdict: string): ManagerVerdictState {
  return MANAGER_VERDICT_META[verdict]?.state ?? 'idle'
}
