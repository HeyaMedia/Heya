const DEVICE_ID_KEY = 'heya_device_id'
export function clientDeviceID() {
  if (import.meta.server) return 'client:ssr'
  let id = localStorage.getItem(DEVICE_ID_KEY)
  if (!id) { id = `client:${crypto.randomUUID()}`; localStorage.setItem(DEVICE_ID_KEY, id) }
  return id
}
export function clientDeviceKind() {
  if (/iPad|Tablet/i.test(navigator.userAgent)) return 'tablet'
  if (/Mobile|Android|iPhone/i.test(navigator.userAgent)) return 'phone'
  return 'computer'
}
export function clientDeviceName() {
  const platform = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform || navigator.platform
  const browser = /Firefox/i.test(navigator.userAgent) ? 'Firefox' : /Edg/i.test(navigator.userAgent) ? 'Edge' : /Chrome/i.test(navigator.userAgent) ? 'Chrome' : /Safari/i.test(navigator.userAgent) ? 'Safari' : 'Browser'
  return `${platform || clientDeviceKind()} · ${browser}`
}
