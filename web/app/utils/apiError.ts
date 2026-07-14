export function apiErrorMessage(error: unknown, fallback: string): string {
  const e = error as any
  const value = e?.data?.detail
    || e?.data?.error
    || e?.data?.message
    || e?.response?._data?.detail
    || e?.response?._data?.error
    || e?.message
  return typeof value === 'string' && value.trim() ? value : fallback
}
