# System media integration

The frontend owns one system-media coordinator and selects a single adapter at
runtime. Normal browser and installed-PWA sessions use web platform APIs.
HeyaClient sessions use its origin-scoped native bridge instead, avoiding two
competing media sessions or duplicate notifications.

## Browser and PWA

The Media Session adapter publishes title, artist, album, artwork, duration,
position, and the playing state. Play, pause, previous, next, stop, and seek
actions go through the normal player bindings, so they continue to work with
browser, native-audio, and cast output.

Track notifications use the Notifications API and are disabled by default.
The user must enable them under Settings → Device, which is the only place the
frontend requests browser permission. A notification appears only after a real
post-startup item change while the page is hidden or unfocused. The service
worker click handler focuses an existing Heya window and opens `/music`.

## HeyaClient

The native adapter waits for HeyaClient's protocol-v1 capability handshake,
then publishes normalized, revisioned snapshots through
`window.__HEYA_SYSTEM_MEDIA__`. Hardware and OS media commands are subscribed
through the bridge and routed into the same player bindings as browser Media
Session actions.

Artwork is fetched from the same authenticated origin, drawn into a square
canvas no larger than 512 pixels, encoded as JPEG, and bounded to 512 KiB
before being sent to the native client. HeyaClient validates and caches it
again. Track-change notifications are controlled by HeyaClient's local
Cmd/Ctrl+, settings rather than the remote device preference.

The frontend bridge type is in
[`web/app/types/system-media.ts`](../web/app/types/system-media.ts). The native
security boundary and OS adapters are documented in the HeyaClient repository.
