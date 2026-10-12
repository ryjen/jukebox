# Read-only Music Assistant compatibility probe

The `ma-probe` CLI checks the **current configured room/player** and federated catalogue through the pinned Music Assistant 2.10.6 HTTP API. It never modifies playback, queue state, provider configuration, room grouping or user permissions. It does **not** assert that the returned search results cover both local and cloud sources.

## Preconditions

- An operator has explicitly started a test Music Assistant 2.10.6 instance (see [MA lab](ma-lab.md)). Nothing in this command starts a Music Assistant server.
- The operator has a dedicated token with exactly the read scopes the Jukebox adapter requires: `PLAYERS_READ`, `QUEUES_READ`, and `LIBRARY_READ`. **Do not use an admin token.**
- The named `JUKEBOX_MA_PLAYER_ID` is a real, available, **independent** player. This probe refuses group owners, synced children and other ambiguous group states.
- A search term is chosen that can be safely sent to the configured search providers. For meaningful coverage, test a representative track in the local library, then a track known to exist in a configured streaming provider.

## Use

The server must be on HTTPS or a literal loopback HTTP address, and the player ID / query are supplied by the operator:

```sh
export JUKEBOX_MA_URL=http://127.0.0.1:8095
export JUKEBOX_MA_PLAYER_ID=your-independent-test-player
export JUKEBOX_MA_SEARCH_QUERY="an authorized test song"
read -r -s -p "Music Assistant read-only bearer: " JUKEBOX_MA_TOKEN
printf '\n'
export JUKEBOX_MA_TOKEN
go run ./cmd/ma-probe
unset JUKEBOX_MA_TOKEN
```

The invocation sends only `players/get`, `player_queues/get_active_queue`, `player_queues/items`, and `music/search`. It outputs **only** machine-readable status, bounded queue and search counts, and stable failure categories. It never prints a track name, player ID, bearer, provider URI, or raw upstream error. On success the output resembles:

```json
{"status":"pass","queue":"ok","queueItems":2,"search":"ok","searchMatches":7}
```

Exit codes: `0` for both requests successfully parsed (results may be empty), `1` for an upstream failure or unauthorized/grouped player, and `2` for missing/invalid local configuration. Failed probes **do not** grant a fallback privilege or enable queued playback.

For the live evidence ledger, record only the exact upstream server revision/image digest, command version, scopes used, response categories and counts. Run again with distinct local and cloud search terms. This is **not** proof of separate provider coverage until Music Assistant’s admin UI or a provider-specific read-only check independently confirms the matching source.

## Security and limitations

- Readiness is checked at the API boundary; no LAN or public ingress is configured here.
- A passing mock fixture is source evidence; a passing probe against the pinned server is real API/permission evidence, **not** a provider entitlement, playable-track, multiroom grouping or no-autoplay guarantee.
- The probe fails closed on group membership and on unexpected response shapes.
- It performs at most two player reads, one queue read, one queue-items read and one search. The upstream client has a 5-second HTTP timeout on each operation and no redirects.
- This does not change Music Assistant permissions, install a service, or make Jukebox remotely reachable.
- Avoid writing bearer values to terminal history, CI logs, issue comments, screenshots, or source files.

If the server is not available, do not guess results. Mark live compatibility **blocked** in #3/#9/#11 and keep real queue writes disabled.
