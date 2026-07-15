// Promise-based text-input dialog replacement for window.prompt().
//
// Usage:
//   const { promptText } = usePrompt()
//   const name = await promptText({ title: 'Rename playlist', label: 'Name', initial: cur })
//   if (name === null) return          // cancelled (Escape / Cancel / overlay)
//   // name is the trimmed string (or '' when allowEmpty)
//
// Mount <TextPromptDialog /> once (app.vue, alongside TrackInfoDialog) — it
// consumes this shared module-level state and renders the AppDialog. A single
// outstanding request at a time; a new call resolves any open one as cancelled
// (null), matching window.prompt() semantics.
//
// Named `promptText` (not `prompt`) so it never shadows or reads as the native
// window.prompt(), and so the "no native prompt" grep stays clean.

interface PromptOptions {
  title: string
  /** Mono field label above the input (2.0 grammar). */
  label?: string
  /** Optional helper line under the input. */
  message?: string
  /** Pre-filled value; the input is selected on open so typing replaces it. */
  initial?: string
  placeholder?: string
  confirmLabel?: string
  cancelLabel?: string
  /** When true, an empty submit resolves '' instead of being blocked — used
   *  for the tags editor where clearing the field is a valid action. */
  allowEmpty?: boolean
  maxLength?: number
}

interface PromptState extends PromptOptions {
  open: boolean
  resolve?: (value: string | null) => void
}

const state = ref<PromptState>({
  open: false,
  title: '',
})

export function usePrompt() {
  function promptText(opts: PromptOptions): Promise<string | null> {
    return new Promise<string | null>((resolve) => {
      // A still-open prompt is resolved as cancelled before we replace it,
      // so no Promise is left dangling.
      if (state.value.open && state.value.resolve) {
        state.value.resolve(null)
      }
      state.value = {
        open: true,
        title: opts.title,
        label: opts.label,
        message: opts.message,
        initial: opts.initial ?? '',
        placeholder: opts.placeholder,
        confirmLabel: opts.confirmLabel ?? 'Save',
        cancelLabel: opts.cancelLabel ?? 'Cancel',
        allowEmpty: opts.allowEmpty ?? false,
        maxLength: opts.maxLength ?? 200,
        resolve,
      }
    })
  }

  function _resolve(value: string | null) {
    if (state.value.resolve) state.value.resolve(value)
    state.value = { ...state.value, open: false, resolve: undefined }
  }

  return { promptText, state, _resolve }
}
