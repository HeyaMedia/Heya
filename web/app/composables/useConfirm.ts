// Promise-based confirm dialog replacement for window.confirm().
//
// Usage:
//   if (!await useConfirm().confirm({ title: 'Delete?', message: 'This cannot be undone.', destructive: true })) return
//
// Mount <ConfirmDialog /> once in the default layout — it consumes the same
// shared state and renders a reka-ui AlertDialog. State is a single
// outstanding request at a time; concurrent calls overwrite each other,
// which matches window.confirm() semantics.

interface ConfirmOptions {
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
}

interface ConfirmState extends ConfirmOptions {
  open: boolean
  resolve?: (value: boolean) => void
}

const state = ref<ConfirmState>({
  open: false,
  title: '',
})

export function useConfirm() {
  function confirm(opts: ConfirmOptions): Promise<boolean> {
    return new Promise<boolean>((resolve) => {
      // If a previous prompt is still open, resolve it as cancelled before
      // replacing — avoids leaving a dangling Promise.
      if (state.value.open && state.value.resolve) {
        state.value.resolve(false)
      }
      state.value = {
        open: true,
        title: opts.title,
        message: opts.message,
        confirmLabel: opts.confirmLabel ?? 'Confirm',
        cancelLabel: opts.cancelLabel ?? 'Cancel',
        destructive: opts.destructive ?? false,
        resolve,
      }
    })
  }

  function _resolve(value: boolean) {
    if (state.value.resolve) state.value.resolve(value)
    state.value = { ...state.value, open: false, resolve: undefined }
  }

  return { confirm, state, _resolve }
}
