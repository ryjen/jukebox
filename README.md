# Jukebox

Experimental local-first **multiroom digital jukebox** for phones and browsers.

**Current status:** a working, nonpersistent room-scoped *request-board prototype*. It **does not** search a real music catalogue, enqueue music in Music Assistant, control audio, group speakers, or play songs. These are [tracked delivery milestones](https://github.com/ryjen/jukebox/issues/1).

## Product direction

- Reuse [Music Assistant](https://music-assistant.io/) as the authority for local/cloud catalogues, physical players, their queues and synchronized playback.
- Reuse/upstream Music Assistant Party functionality where feasible; no maintained server fork by default.
- Expose room-scoped guest sessions with independent room queues, eventual temporary grouping, and a responsive phone/web interface.
- Prefer offline-capable local playback; no subscription dependence for owned local media.

## Run the experimental request board

Requires Go 1.23+; no third-party packages. On a development machine:

```sh
# Run in the repository root. Supply your own strong secret; do not commit it.
export JUKEBOX_ADMIN_TOKEN="$(openssl rand -hex 32)"
go run ./cmd/jukebox
# Open http://127.0.0.1:8108 in a browser
```

To create an **independent one-time** invitation for the `living-room` room:

```sh
curl --fail-with-body -sS \
  -H "Authorization: Bearer $JUKEBOX_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"roomId":"living-room"}' \
  http://127.0.0.1:8108/api/admin/invites
```

Paste the `inviteCode` into the web page. It expires in ten minutes; the exchanged session is bound to one room for eight hours. Issue a different invitation for the `office` room. The room-to-player mapping in [`config.example.json`](config.example.json) is **declarative only** until Music Assistant integration.

Hosts can view active room sessions without accessing guest bearer tokens, then revoke a session by its admin-visible ID:

```sh
curl -H "Authorization: Bearer $JUKEBOX_ADMIN_TOKEN" \
  http://127.0.0.1:8108/api/admin/sessions
curl -X POST -H "Authorization: Bearer $JUKEBOX_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"sessionId":"<id-from-list>"}' \
  http://127.0.0.1:8108/api/admin/revoke
```

`JUKEBOX_CONFIG` can point to a custom JSON configuration. `JUKEBOX_LISTEN_ADDR` defaults to loopback. Non-loopback binding requires the explicit `JUKEBOX_ALLOW_REMOTE=1` opt-in, but this is **not a substitute for an authenticated HTTPS reverse proxy and network isolation**. Do not deploy this experimental prototype directly on a public interface.

### Verify

```sh
go test -race ./...
go vet ./...
```

## Optional read-only Music Assistant 2.10.6 integration

This experimental preview makes **no playback changes**. It reads the configured room player's active queue, and rejects unapproved group queues until room/group ownership is implemented. Guest access is still room-bound.

```sh
# Configure a read-only token with QUEUES_READ, PLAYERS_READ and LIBRARY_READ in Music Assistant.
# Never expose it to browsers or commit it to source.
export JUKEBOX_MA_URL=http://127.0.0.1:8095
export JUKEBOX_MA_TOKEN="your-read-only-token"
go run ./cmd/jukebox
```

The token requires `QUEUES_READ` and `PLAYERS_READ`, since detecting native synchronized group coordinators requires the player's actual membership, not just its queue ID. The server uses the pinned Music Assistant 2.10.6 `POST /api` HTTP command transport for `player_queues/get_active_queue` and `player_queues/items`, with a maximum of 25 queue items. In the web room view, select **Refresh playback**. A client may also call `GET /api/rooms/<room-id>/queue` using its room bearer. Without both configuration values this route returns 503; invalid upstream responses return 502, and unexpected group queues return 409. Requests submitted to the request board are **never** transmitted to Music Assistant.

Music Assistant's `music/search` command is also available in a **read-only catalogue preview** through `GET /api/rooms/<room-id>/search?q=<query>`. The token must additionally have `LIBRARY_READ` scope. Search covers the local library and configured cloud providers; Jukebox projects only track URI, name, artist and provider. Queries are limited to 2–80 characters, 15 displayed results and 40 attempts per room session. Selecting a search result only copies metadata into the local request form—it does not enqueue playback. Cloud entitlements and provider availability are governed by Music Assistant and must be verified on a real instance.

The URL requires HTTPS unless the target is a literal loopback IP. Configure Jukebox and Music Assistant on a private network, and ensure their server-side credentials are restricted. There are no authenticated live Music Assistant tests yet; the protocol is validated using in-process fixtures.

For a non-mutating, redacted live compatibility check of the pinned Music Assistant version, see [the read-only MA probe](docs/operations/ma-readonly-probe.md) (`go run ./cmd/ma-probe`). This probe never starts audio or modifies queues. Passing fixtures alone does not satisfy the real-instance integration gate.\n\n## Trust and operational limitations

The host bearer is only read server-side; guest invite exchange is single-use and sessions are scoped to exactly one room. Room requests are bounded in memory and guest requests are **not** verified against music providers. Restart loses invites, sessions and requests. There is no persisted audit trail, token hashing, distributed limiting or production-grade reverse-proxy deployment.

This public repository does **not currently declare a redistribution license**. Licensing is an explicit unresolved bootstrap decision; do not assume a permissive open-source license.

Architecture and current limitations: [`docs/architecture/README.md`](docs/architecture/README.md). Delivery order: [epic #1](https://github.com/ryjen/jukebox/issues/1). Engineering policy: [Micrantha](https://github.com/hackelia-micrantha/.github).
