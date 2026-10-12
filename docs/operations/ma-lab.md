# Music Assistant 2.10.6 — isolated integration lab

**Status:** manual, disposable integration recipe. Not executed on Dubnium. This document does not grant production deployment or host activation authority.

## Why a separate port matters

Music Assistant serves its UI/API on TCP **8095** and audio streaming on TCP **8097**. Jukebox's new default `127.0.0.1:8108` intentionally avoids both. Music Assistant needs **host networking** for discovery and streaming to physical players on the same layer-2 network. A bridge network with just two mapped ports is insufficient for AirPlay, Sonos, Chromecast or DLNA devices.

Sources: [official installation instructions](https://www.music-assistant.io/installation/) and [official 2.10.6 image](https://github.com/music-assistant/server/pkgs/container/server).

## Pinned test artefact

- Image: `ghcr.io/music-assistant/server:2.10.6`
- Published multi-arch manifest: `sha256:45fdb050e06962097a07feeb092606ad119fac17965c8228a57146448d5fa772`.
- Upstream annotated git tag `2.10.6` resolves to commit `c24866405bd2068c25a60e1014e5ef8c3f6445bb`.
- Template: [deploy/compose.music-assistant.lab.yaml](../../deploy/compose.music-assistant.lab.yaml).

The tag and manifest were retrieved from GitHub at planning time; the actual image's manifest, architecture and source revision must be rechecked after pulling in the target environment. No unpinned `latest` image is accepted for admission evidence.

## Manual test setup

1. Choose a writable private data directory and an existing music folder. Do not copy or commit streaming credentials or proprietary audio into this repository. On a host with Podman and a compose provider:

```sh
export JUKEBOX_MA_DATA_DIR="$HOME/.local/state/jukebox/ma-lab"
export JUKEBOX_MUSIC_DIR="/absolute/path/to/your/music"
mkdir -p "$JUKEBOX_MA_DATA_DIR"
podman compose -f deploy/compose.music-assistant.lab.yaml up -d
```

2. Open `http://<lab-host>:8095` from an authorized LAN device to complete Music Assistant's first-run setup. Host networking exposes its default listeners directly; restrict the host firewall and do not expose its admin interface on the public internet. A locally reachable test instance is not equivalent to an authorized production deployment.
3. Configure a **local-files** music source pointed at the read-only `/media` mount and **one streaming source** for which the owner is entitled to playback. Validate both sources inside Music Assistant first, including actual provider terms. No sample credentials are supplied.
4. Create a dedicated Music Assistant account/token with only `PLAYERS_READ`, `QUEUES_READ` and `LIBRARY_READ` scopes, if supported by the installed version. Do not use an admin token for the Jukebox integration. If this least-privilege account cannot be provisioned, keep integration disabled and report the blocker on #3.
5. Point the Jukebox process at `http://127.0.0.1:8095` (same host) and inject its token as `JUKEBOX_MA_TOKEN` via an operator-controlled secret source. Jukebox itself uses loopback TCP 8108 by default. Do not put the bearer in shell history, compose files, browser storage, commit messages, logs or screenshots.

## Validation matrix

Use two separately authorized Jukebox room sessions:

| Probe | Expected result |
| --- | --- |
| Music Assistant startup and image identity | Report exact tag, digest, architecture and running API version |
| `players/get` with read token | Returns configured player state, denied without `PLAYERS_READ` |
| `player_queues/get_active_queue` and `/items` | Only the room-mapped independent player; 25 items beginning at current playhead |
| Grouped player with same queue ID | Jukebox refuses with HTTP 409; no cross-group item disclosure |
| `music/search` with local and cloud result | Track identities/metadata return, unsupported providers/errors fail safely |
| Expired/revoked guest / other room | HTTP 403; no privileged upstream call |
| Upstream stopped or token revoked | Bounded 502 and no guessed cached playback state |
| Idle queue | Viewing the queue must **not** start audio or change the queue |
| Restart and rollback | Persistent MA settings on the lab volume; no automatic Jukebox host activation |

Treat the read-only adapter's fixture tests as **source evidence**, not proof of Music Assistant compatibility. Record real-instance results, failures and exact provider/player versions in #3/#9/#11. Do not introduce queue writes until the group-authorization and autoplay design in #4/#5 is reviewed.

## Shutdown and recovery

```sh
podman compose -f deploy/compose.music-assistant.lab.yaml down
```

The separate `JUKEBOX_MA_DATA_DIR` remains on disk for explicit inspection or later deletion; the command does not purge music files or credentials. Stop the lab before activating another server that requires host ports 8095 or 8097.

**Operational boundary:** the upstream explicitly supports the Docker installation described in its docs. Rootless Podman compatibility and networking require target-host validation. This template is not permission to change Dubnium firewall, network services, privileged runners or playback routing.
