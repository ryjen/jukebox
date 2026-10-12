# ADR 0001 — Reuse Party behaviour without sharing multiroom authority

**Status:** Proposed for write operations; adopted for the existing read-only Jukebox slices.  
**Context:** #3 (Music Assistant contract), #4 (real queue writes), #5 (grouping); [upstream 2.10.6 source](https://github.com/music-assistant/server/tree/c24866405bd2068c25a60e1014e5ef8c3f6445bb).

## Problem

The digital jukebox must support independent, concurrent room sessions, a single aggregated local/cloud library, and optional synchronized groups. The user prefers customizing/reusing Music Assistant's Party plugin rather than inventing an independent music server.

Music Assistant **2.10.6 Party** currently has `multi_instance: false`; `party/player` chooses one player/queue and `party/add_to_queue(uri, boost)` has no room argument. On an idle queue the upstream implementation explicitly calls `load` followed by `play_index`, starting playback. A guest Party credential has `Scope.QUEUES_CONTROL`; giving it to Jukebox browsers would not satisfy strict room authorization.

## Decision

1. **Music Assistant owns** catalogue aggregation, provider credentials/entitlements, media resolution, real player/queue state, streaming, player discovery and audio synchronization.
2. **Jukebox owns** room identity and access policy, invitations, per-room fairness/quota, request-intent lifecycle, and an adapter that maps a server-approved room to a Music Assistant player/group queue.
3. **Use Party upstream unmodified for a single-room party where it fits.** For independent simultaneous rooms, do not route guests through `party/add_to_queue` or expose Music Assistant's `QUEUES_CONTROL` scope. Reuse Party's documented interaction concepts and pursue an upstream extension for multi-instance, room-scoped party sessions when feasible. Do not copy upstream source into Jukebox or maintain an unconditional frontend fork.
4. Keep **read-only operations enabled independently** of writes: `players/get` (`PLAYERS_READ`), `player_queues/get_active_queue`, `player_queues/items` (`QUEUES_READ`), and federated `music/search` (`LIBRARY_READ`). Code must reject group-state ambiguity, failed validation and unrecognized upstream responses.
5. Any future **queue write capability is explicitly off by default** until a pinned Music Assistant 2.10.6 instance proves command semantics, scoped credentials, queue ownership, provider identity and audio side effects. No host authority is derived from merge, test pass, or this ADR.

## Queuing and authorization invariants for #4

- Browser sends a room session and an immutable **canonical media identity** derived from a real catalogue search, not a trusted display title, artist string or arbitrary URL.
- The server re-resolves and validates availability/entitlement, then binds the authorized room to its current independent player/queue. Group membership must be checked explicitly; a coordinator's queue ID can equal its own player ID while other rooms are grouped.
- The service credential needed for Music Assistant queue writes stays server-side and separate from guest session tokens. A coarse upstream `QUEUES_CONTROL` scope does not confer cross-room user authority; Jukebox must deny any command not required by its narrowly documented adapter.
- Queue intent must have a stable idempotency key and explicit state (`requested`, `submitted`, `confirmed`, `unknown`, `rejected`). Do not treat HTTP timeout as proof that a write failed; reconcile actual queue contents before retrying. Exactly-once queue writes must not be claimed without a transaction or equivalent upstream guarantee.
- A user request becomes **queued** only after the Music Assistant queue reflects the canonical media item in the authorized room. Track playback errors separately from request acceptance.
- **Autoplay defaults off** for office/host-governed mode. Starting or resuming audio requires an explicit room-host policy tested against an idle queue; no generic reuse of Party's idle-queue autoplay behavior.
- Rate limits, duplicate prevention and fairness are enforced on the server; browsers may visualize or vote but cannot act as authoritative limiters.
- On crash/restart, reconcile unconfirmed intents against Music Assistant and preserve evidence of denied or ambiguous operations; never silently replay mutation requests.

## Room grouping invariants for #5

- Independent rooms own independent queue intents and playback state. A group uses a single authoritative group/leader queue while grouped; no independent per-room enqueue to the group's members.
- Joining a group explicitly records or suspends the original queues, obtains authorization for *every* member, and rejects conflicting ownership. Do not silently concatenate or discard queue state.
- Ungrouping restores pre-group queue intentions without automatically resuming audio. Failures must leave a recoverable, diagnosable ownership state.
- Locking only inside Jukebox cannot make an external Music Assistant player/group change atomic. Compare state before and after mutations and fail closed on conflict; seek an upstream transactional or controlled interface if races remain unsafe.
- Synchronized playback is only promised for compatible players/groups verified on actual hardware. MPD or arbitrary heterogeneous software players are not presumed synchronized.

## Alternatives

| Alternative | Decision | Why |
| --- | --- | --- |
| One Music Assistant/Party server for each room | Reject | Duplicates providers/configuration, complicates grouping and administration |
| Fork Party's guest frontend permanently | Defer | Upstream UI and API changes create ongoing divergence |
| Let each guest call Music Assistant directly | Reject | Guest control scopes are not sufficiently room-bound |
| Single MA server + Jukebox room adapter + optional upstream contribution | **Preferred** | Reuses MA catalogue/playback while keeping room authorization in one place |

## Required proof before write implementation

1. Reproduce `players/get`, `player_queues/get_active_queue`, `player_queues/items` and `music/search` against the **pinned** Music Assistant 2.10.6 server with a least-privileged read-only token; verify local and cloud providers separately.
2. Create an isolated test player with no real speakers, or another explicitly authorized safe target. Exercise the candidate queue enqueue command with playing, paused, idle, grouped, unavailable and already-buffered states; record whether it starts audio, moves items or replaces a queue.
3. Prove denied cross-room writes, rate limits, revocation, canonical URI resolution, idempotency recovery, concurrent operations and provider failures on the composed exact version.
4. Decide between an upstream Party multi-instance change and a bounded companion adapter for writes based on that evidence; amend this ADR before enabling the write path.
5. Follow Micrantha test-pyramid, fail-safe merge, and Dubnium critical-runner/host activation boundaries before deployment. No production credentials in tests or documentation.

**Unresolved:** Exact write API and `QueueOption` semantics, atomic group ownership, persistence model, provider-specific playback entitlements and office public-performance rights. These are blockers for *enabling writes and deployment*, not for shipping the current read-only source.
