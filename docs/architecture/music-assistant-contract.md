# Music Assistant integration contract — research baseline

**Status:** research baseline only; no working Music Assistant adapter, provider authentication, catalogue browsing or playback. Tracks [#3](https://github.com/ryjen/jukebox/issues/3).

## Exact upstream subject

- Upstream project: [music-assistant/server](https://github.com/music-assistant/server).
- Candidate supported release: **2.10.6** (published 2026-10-09); annotated tag resolves to commit `c24866405bd2068c25a60e1014e5ef8c3f6445bb`. Use that commit for subsequent API/fixture tests, not floating `dev`.
- Main upstream server license: Apache-2.0 ([license at tag](https://github.com/music-assistant/server/blob/2.10.6/LICENSE)). Do not interpret this as a license grant from every separate music provider.
- Party source: [manifest](https://github.com/music-assistant/server/blob/c24866405bd2068c25a60e1014e5ef8c3f6445bb/music_assistant/providers/party/manifest.json) and [provider implementation](https://github.com/music-assistant/server/blob/c24866405bd2068c25a60e1014e5ef8c3f6445bb/music_assistant/providers/party/__init__.py).

## Verified API and policy observations

| Upstream command or component | Observed property at 2.10.6 | Jukebox implication |
| --- | --- | --- |
| Party manifest | `multi_instance: false` | Cannot configure an independent Party instance per room. |
| `party/player` | Selects a configured/automatic *single* queue | Must not use as an arbitrary room selector. |
| `party/add_to_queue(uri, boost)` | Requires `Scope.QUEUES_CONTROL`; no `roomId` or `queueId` parameter | Cannot safely expose directly as cross-room guest command. |
| `party/add_to_queue` on an idle queue | Calls queue `load` followed by `play_index` | Guest requests may unexpectedly start audio. Require explicit host autoplay policy. |
| `player_queues/get(queue_id)` | `Scope.QUEUES_READ` | Candidate server-side read-only probe. |
| `player_queues/items(queue_id, limit, offset)` | `Scope.QUEUES_READ` | Candidate room queue read. |
| `player_queues/play_media(queue_id, media, option,...)` | `Scope.QUEUES_CONTROL`; handles playable-source selection, not just queue items | Dangerous as a generic guest write. Do not use before option/side-effect contract tests. |
| Party guest helper | Creates guest identity and join codes; revocation disconnects guest WebSockets | Reuse principles / contribute upstream where feasible, but do not bypass room-scoped access. |

Upstream [queue controller source](https://github.com/music-assistant/server/blob/c24866405bd2068c25a60e1014e5ef8c3f6445bb/music_assistant/controllers/player_queues/controller.py) and [guest helper source](https://github.com/music-assistant/server/blob/c24866405bd2068c25a60e1014e5ef8c3f6445bb/music_assistant/helpers/guest_access.py) provide the version-bound references. Source inspection proves method signatures and declared scopes, **not** stable network transport, queue correctness, or real-provider functionality.

## Candidate direction — requires integration proof

Use a **Jukebox-owned server-side room-authorizing adapter**, delegating catalogue and queues to Music Assistant and keeping the upstream Party implementation unchanged. Reuse Party guest UX/semantics only when boundaries are compatible, and propose an upstream room-scoped extension rather than copying implementation code.

A guest may supply a requested track identity, but the adapter must look up a canonical Music Assistant media item, enforce the currently authorized room/queue, validate the selected player/group ownership, and confirm actual queue state before acknowledging `queued`. Music Assistant credentials are never shipped to the browser.

**Before choosing a write API:** prove the transport/authentication mechanism and queue command shape against the pinned release. `party/add_to_queue` is **not** approved for multiroom writes. Any write-capable backend needs per-room serialization, replay protection/idempotency, explicit autoplay policy and failure recovery. Prefer narrowly scoped upstream changes where practical.

## Required executable evidence for #3

1. Run a local test Music Assistant **2.10.6** instance (or identical pinned commit); record image digest and the actual provider/frontend versions.
2. Establish an authenticated read-only test against `player_queues/get` and `player_queues/items`; verify queue identifiers and permission denial. No guest token gains `QUEUES_CONTROL`.
3. Verify local and cloud library search with at least one supported provider each; record media mapping and entitlement failures.
4. In an isolated test queue, exercise queued-vs-autoplay semantics, duplicate suppression, loss of connection, invalid media URI, guest revocation, and simultaneous requests. Restore source queue state afterward.
5. Compare the upstream multi-instance contribution versus the companion adapter, document the outcome in an ADR, and add fail-closed contract tests before starting [#4](https://github.com/ryjen/jukebox/issues/4).

## Out of scope

No actual playback implementation, source copy, network exposure, unbounded scope grants, provider credentials, production licensing assertion, or deployment is introduced by this document.
