# Jukebox

Experimental, local-first multiroom digital jukebox for phones and browsers.

**Status:** repository initialization only. There is no deployable jukebox yet.

## Direction

- Reuse [Music Assistant](https://music-assistant.io/) for the unified local/cloud catalogue, playback and synchronization.
- Reuse the upstream Party plugin when possible; do not copy its implementation into this repository.
- Support room-scoped guest requests and independent queues, with optional synchronized room groups.
- Keep streaming credentials and playback administration outside the guest trust boundary.
- Deploy separately through the Dubnium infrastructure when integration is ready.

Implementation, license, and distribution decisions will be recorded in the repository rather than inferred from its public visibility.

Engineering conventions: [Micrantha](https://github.com/hackelia-micrantha/.github/blob/main/docs/prompts/README.md).