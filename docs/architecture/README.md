# Architecture and delivery boundaries

Status: **experimental request-board prototype**. This repository contains a fixture-tested **read-only Music Assistant HTTP adapter** and local/cloud search preview. It does not yet perform player control, music enqueue, cloud authentication itself, queue migration, audio synchronization, QR generation or production persistence.

## Responsibility split

| Component | Owns | Does not own |
| --- | --- | --- |
| Jukebox | Room identity, guest authorization, request intentions and future queue/group policies | Stream decoding, DRM, music entitlement decisions |
| Music Assistant (upstream) | Local/cloud catalogue, provider mapping, playback queues, player discovery, synchronization | Jukebox guest/host identity and cross-room policy |
| Dubnium (future integration) | OCI deployment and infrastructure-level monitoring/recovery | Business rules or media authorization |
| musicpkg (future) | Owner-bound cryptographic distribution | Current player entitlement or requests |

## Confirmed upstream constraint

The Party plugin is in [`music-assistant/server/music_assistant/providers/party`](https://github.com/music-assistant/server/tree/dev/music_assistant/providers/party) and has `multi_instance: false` on the inspected development manifest (2026-10-11). Do not assume one Party instance per room works. Evaluate a small room-aware upstream change before implementing a wrapper; do not vendor upstream source.

## Proposed future contract

- Room identifiers are stable logical authorization scopes; players are physical playback targets.
- Music Assistant remains the **only playback/queue authority**. A request must not claim queued status until confirmed by the current target player's queue.
- Search results produce signed, ten-minute room/session-bound catalogue choices. A selected request retains its upstream provider URI **server-side**; a free-text suggestion has no canonical identity and cannot be promoted to a playback command.
- Media identities are Music Assistant provider mappings, not guest-entered strings.
- During grouping, a room's independent queue is suspended transactionally or preserved as a snapshot; no silent merge or autoplay after detach.
- A sync group has an explicit queue owner; reject conflicting updates, stale snapshots, incompatible players, and ambiguous partial group operations.
- A guest capability is bound to one room and a limited lifetime. A group does not expand those rights automatically.
- API credentials stay in trusted server configuration; guests cannot invoke administrator/player-control APIs.

## Prototype trust model

- A high-entropy host token is supplied at startup (no built-in default). Host creates one-time, 10-minute invites by API.
- A valid invite exchanges for a random 8-hour room-bound bearer, held only in the browser tab session store.
- A guest can read/submit up to five **request intentions** to their room; at most 50 are stored per room. No provider lookup or playback occurs.
- The in-memory store serializes requests but has **no persistence, distributed consistency, meaningful per-device anti-abuse, global rate limiting or automatic expiry garbage collection**. It is not suitable for untrusted public-network exposure.
- Hosts enumerate active sessions by non-secret session ID via `GET /api/admin/sessions`, then revoke with `POST /api/admin/revoke` and a `sessionId`. The admin API never returns guest bearer tokens. Closing a tab only forgets the local bearer, not revokes it.
- Bind to numeric loopback addresses **only**. No opt-in to direct LAN/plain HTTP binding exists. Any exposed deployment needs a separately reviewed TLS/authenticated reverse proxy, network boundary and end-to-end device tests.
- The prototype deliberately does not accept a guest URL or query-string credential.

## Decision and release gates

- Licensing is unresolved; public source does not imply an open-source grant. Do not publish distributable release artifacts until this is addressed.
- Real provider APIs, Party compatibility, local/cloud entitlements and office performance licenses must be verified against a pinned server release.
- A future production release requires token hashing at rest, persistent bounded state, proper session lifecycle, invitation/room management UI, rate limiting across hosts, CSRF/network threat model and operational backup/recovery evidence.
