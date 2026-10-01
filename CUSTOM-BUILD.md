# Run this modified bridge with Docker Compose

This build adds two settings under `bridge` in `/data/config.yaml`:

```yaml
bridge:
    klipy_gifs: true
    preserve_deleted_messages: true
```

They default to true in the updated example config and are added during config
upgrades. Klipy MP4 video embeds become looping GIF images (15 fps, at most
480 pixels per side). FFmpeg is already included in the Docker image. Conversion
errors appear as media failure notices. Previously posted Matrix videos are not
changed. Discord deletions, including bulk deletions, become edits with a
**Deleted message** header and the elapsed time since posting, retaining text and media. Matrix-origin deletions
still delete the Discord message. A failed fetch or edit leaves the Matrix
message intact and logs an error.

Copy this working tree to your server. From this repository on your local machine,
replace the server and destination paths below with your own:

```sh
rsync -av --exclude=.git ./ user@server:/opt/mautrix-discord-custom/
```

In your existing Compose file, change the bridge service's `image` and add
`build` and `pull_policy`. Keep your existing volumes, networks, ports, environment,
and other settings. The service name below is an example:

```yaml
services:
  discord:
    image: mautrix-discord-custom:local
    build:
      context: /opt/mautrix-discord-custom
    pull_policy: never
    # Keep your existing /data mount and all other service settings here.
```

On the server, from the directory containing your Compose file:

```sh
docker compose build discord
docker compose up -d --no-deps discord
docker compose logs --tail=100 discord
```

Use your actual service name in place of `discord`. Rebuilding is necessary after
copying further source changes. Keep the same `/data` volume and database so that
registration, login sessions, room mappings, and media remain available. Take a
backup of the configuration and database before replacing the running bridge.
Do not generate a new registration or run `docker compose down -v`.

To roll back, restore your previous `image`, remove `build` and `pull_policy`, and
run `docker compose up -d --no-deps discord` again.

GIF stickers are converted to animated PNG using FFmpeg, including when direct
media is enabled. Animation is preserved. WebP GIF embed updates retain the
original Matrix media event. Deletion delays use the original Discord posting
time and the time the bridge receives the deletion; delivery delays or downtime
can therefore make the displayed duration longer than the actual lifetime.

WebP media embeds use their original WebP URL and convert to looping GIFs only when their RIFF chunks indicate animation, using
ImageMagick, which is included in the updated Docker image. This bypasses still
thumbnail proxies and uses a separate conversion cache entry. Static WebPs retain
their original bytes and format. GIF sticker URLs
use Discord's media host rather than the CDN. Rebuild the image to install the new
dependency. These changes affect newly bridged media, not existing events.

If an original WebP download or conversion fails, the bridge falls back to
Discord's thumbnail proxy when available. This may be static, but avoids a media
failure notice when Discord still has a usable preview. Failed transfers are
retried on later messages so upstream recovery does not require a bridge restart.
