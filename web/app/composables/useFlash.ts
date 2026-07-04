// Settings-page flash message state, rendered by <SettingsFlash>.
// Deliberately no auto-dismiss — matching the pages this consolidates, the
// last message stays visible until the next action replaces it.

export type FlashKind = 'ok' | 'err' | 'warn'
export interface FlashMessage { kind: FlashKind, text: string }

export function useFlash() {
  const flash = ref<FlashMessage | null>(null)
  function ok(text: string) { flash.value = { kind: 'ok', text } }
  function err(text: string) { flash.value = { kind: 'err', text } }
  function warn(text: string) { flash.value = { kind: 'warn', text } }
  return { flash, ok, err, warn }
}
