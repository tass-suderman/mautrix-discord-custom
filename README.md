# mautrix-discord-custom

A custom build of the [mautrix-discord](https://github.com/mautrix/discord)
Matrix–Discord puppeting bridge, based on upstream v0.7.7. This fork adds media
conversion and keeps Discord-deleted messages visible in Matrix.

## Custom behavior

### Klipy GIF embeds

Discord's Klipy picker can send MP4 video embeds. With `bridge.klipy_gifs: true`,
recognized MP4 embeds from `klipy.com` and its subdomains, including Discord's
external proxy URLs, become looping GIF images (`m.image`) in Matrix. FFmpeg
converts them at 15 fps, fitting within 480×480 while preserving aspect ratio.
Small inputs may be enlarged. The Matrix filename is `animation.gif`.

This applies to recognized embeds, not arbitrary MP4 attachments or other GIF
providers. Set the option to `false` to keep the existing video behavior.

### Retain deleted Discord messages

With `bridge.preserve_deleted_messages: true`, individual and bulk Discord
message deletions edit the corresponding Matrix messages to retain text and media
with a header such as **Deleted message (4min, 5sec)**. The duration measures the
original posting time to when the bridge handles the deletion; queues and downtime
can increase it. Days and hours appear when needed, and seconds are always shown.

The bridge uses the latest edited content when the homeserver supplies a bundled
replacement relation. Deletion edits clear mentions to avoid new notification
pings. If fetching or editing fails, the Matrix event remains intact and the
bridge logs the failure. Successful edits remove the message's bridge mapping.

Set the option to `false` to restore Discord deletion redactions. Matrix-origin
deletions retain their existing behavior. Attachments actually removed during a
Discord message edit are still redacted.

### Animated stickers

GIF stickers download from Discord's media host and convert to looping animated
PNG with FFmpeg, retaining the `m.sticker` event type. This also works with direct
media enabled, since GIF stickers go through conversion instead of bypassing it.
Lottie stickers use the existing conversion path. Failed sticker downloads or
conversions become ordinary message notices.

There is no new sticker setting. This conversion addresses client compatibility
with GIF sticker events; animated PNG rendering still depends on the client.

### Animated WebP embeds

WebP media embeds prefer their original HTTP(S) WebP URL over Discord's potentially
static thumbnail proxy. Valid RIFF animation chunks trigger ImageMagick conversion
to looping GIFs, with coalesced frames, optimized layers, and a maximum size of
480×480 without enlargement. Static WebPs keep their original bytes and format.

If copying the original or converting it fails, the bridge tries Discord's
thumbnail proxy, then its image proxy when no usable thumbnail URL exists. The
fallback may be static. If the fallback also fails, the normal media failure
notice appears. This applies to the media-embed path, not every WebP attachment or
link preview, and has no separate configuration toggle.

Discord embed updates now use the same message context as initial conversion so
WebP media is not incorrectly classified as removed. Partial updates distinguish
omitted collections from explicitly empty collections for embeds, attachments,
components, and stickers.

### Media spoilers

Discord attachments with the `IS_SPOILER` flag (`flags & 8 != 0`) or a legacy
`SPOILER_` filename are marked with the
[MSC4193 media spoiler](https://github.com/matrix-org/matrix-spec-proposals/pull/4193)
flag, `page.codeberg.everypizza.msc4193.spoiler: true`, including images. This
filename detection is inherited from upstream; this fork adds attachment flag
detection and also preserves the Matrix spoiler flag and its
optional reason in deletion edits. A Matrix client must support media spoilers
to hide the image; a spoiler on the text caption alone does not hide the media.

### Media transfers and limits

Converted Klipy embeds, GIF stickers, and animated WebPs have separate cache and
concurrent-transfer keys from their source representations. A cached original
therefore cannot bypass conversion. Static proxy fallbacks use their ordinary
cache entries. Failed transfers are removed from the in-memory deduplication map,
so later messages can retry a recovered source without restarting the bridge.
Already-failed messages are not automatically retried.

Converters have 60-second timeouts and enforce the homeserver's upload-size limit.
ImageMagick also has memory, map, and disk limits. GIFs can consume substantially
more storage than MP4s. This fork adds no retention policy or database migration;
persistent media cache entries may outlive media removed by homeserver retention.

Changes apply to newly bridged events. They do not restore already-redacted events
or reconvert old media. Deleted sticker rendering, encrypted edit history,
and backfill after deleting message mappings still need broader live validation.

## Configuration

The updated example configuration and normal template-based config upgrades use:

```yaml
bridge:
    klipy_gifs: true
    preserve_deleted_messages: true
    sync_presence: true
```

Each setting can be disabled independently. `sync_presence` forwards Discord
presence and custom status messages to Matrix ghost users: online (including
streaming and mobile online) becomes online, idle/do not disturb become unavailable,
and invisible/offline become offline. Removed custom statuses clear the Matrix
status message. Unicode status emoji are included; custom emoji use `:name:` text.
The Matrix homeserver must enable presence, and clients must support displaying
presence/status messages. Bot logins additionally need the Presence Intent enabled
in the Discord developer portal. Invisible users appear offline to the bridge.
Discord presence is only available for users the gateway exposes to the session.

Docker images include FFmpeg and
ImageMagick; native installations need `ffmpeg` and ImageMagick's `magick` command
in addition to the upstream dependencies.

## Container images

The [Container workflow](.github/workflows/container.yml) tests the bridge with
both media converters, builds the complete Dockerfile, and publishes to GHCR on
pushes to `main` or manual runs. Pull requests build and test without publishing.
Images support **linux/amd64** and **linux/arm64** (including ARM64/v8 hosts).
Separate AMD64 and ARM64 jobs call the same
[reusable build workflow](.github/workflows/container-build.yml) and run in parallel
on native `ubuntu-24.04` and `ubuntu-24.04-arm` runners. Each tests with the media
converters and builds its own image, with a separate cache per architecture.
A final job publishes the combined multi-platform tags only after both builds
succeed. Pull requests test and build both architectures without publishing.

For this repository, tags are:

| Tag | Purpose |
| --- | --- |
| `ghcr.io/tass-suderman/mautrix-discord-custom:latest` | Latest successful build from `main` |
| `ghcr.io/tass-suderman/mautrix-discord-custom:run-<run_id>-<run_attempt>` | A specific Actions run and attempt |
| `ghcr.io/tass-suderman/mautrix-discord-custom:sha-<full_commit_sha>` | Build of a source commit |

The workflow uses `GITHUB_TOKEN` with `packages: write`; no separate registry
secret is needed. The first successful publishing run creates the package.
GitHub packages initially default to private: set the package visibility to public
in its GitHub settings if you want unauthenticated pulls. See GitHub's
[container registry documentation](https://docs.github.com/en/packages/working-with-a-packages-registry/working-with-the-container-registry).

In an existing Compose bridge service, use:

```yaml
services:
  discord:
    image: ghcr.io/tass-suderman/mautrix-discord-custom:latest
    # Keep your existing /data mount, networks, ports, and environment.
```

Remove any old `build` setting or `pull_policy: never` when switching to GHCR.
From your Compose directory, using your actual service name:

```sh
docker compose pull discord
docker compose up -d --no-deps discord
docker compose logs --tail=100 discord
```

Keep the existing database, `/data` volume, configuration, and registration.
Run only one bridge against that database to avoid duplicate messages. The
registration loaded by the homeserver must point to the active bridge's reachable
hostname and port. If the container hostname changes, update the homeserver's
registration URL and reload it as required by your homeserver.

For a reproducible deployment or rollback, use a run tag or image digest. The SHA
tag can be replaced if the same commit is rebuilt. Restore your previous image
reference and run `docker compose up -d --no-deps discord` to roll back.
[Local build instructions](CUSTOM-BUILD.md) remain available.

For ordinary installations without a deliberate status-reporting endpoint, leave
`homeserver.status_endpoint` and `homeserver.message_send_checkpoint_endpoint`
set to `null`.

## Development and validation

Run `go test ./...` with the upstream build dependencies installed. Media conversion
tests require FFmpeg and ImageMagick; they skip relevant portions if those tools
are missing. To test with both tools using Docker:

```sh
docker build --target test -t mautrix-discord-custom:test .
docker build -t mautrix-discord-custom:local .
```

Tests cover media URL recognition, deletion content and duration formatting,
embed-removal handling, WebP RIFF validation, animation frames/timing/looping,
static WebP preservation, GIF-to-APNG conversion, proxy fallback selection, and
spoiler metadata on images and deletion edits.
They do not replace a live Matrix/Discord integration test.

## Upstream documentation and community

Use the upstream documentation for registration, login, bridging, and general setup:

- [Bridge setup](https://docs.mau.fi/bridges/go/setup.html?bridge=discord)
- [Docker setup](https://docs.mau.fi/bridges/general/docker-setup.html?bridge=discord)
- [Authentication](https://docs.mau.fi/bridges/go/discord/authentication.html)
- [Relaying with webhooks](https://docs.mau.fi/bridges/go/discord/relay.html)
- [Features and roadmap](ROADMAP.md)

Upstream discussion: [#discord:maunium.net](https://matrix.to/#/#discord:maunium.net).
