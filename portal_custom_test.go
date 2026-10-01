package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"go.mau.fi/mautrix-discord/database"
	"maunium.net/go/mautrix/event"
)

func TestKlipyMP4(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://static.klipy.com/ii/example.mp4", true},
		{"https://images-ext-1.discordapp.net/external/hash/https/static.klipy.com/ii/example.mp4", true},
		{"https://static.klipy.com/example.mp4?token=x", true},
		{"https://static.klipy.com/example.gif", false},
		{"https://evil.example/klipy.com/example.mp4", false},
		{"https://notklipy.com/example.mp4", false},
		{"https://cdn.discordapp.com/attachments/example.mp4", false},
	} {
		if got := isKlipyMP4(tc.url); got != tc.want {
			t.Errorf("isKlipyMP4(%q) = %v", tc.url, got)
		}
	}
}

func TestDeletedMessageContent(t *testing.T) {
	original := &event.MessageEventContent{MsgType: event.MsgImage, Body: "<test>.gif", URL: "mxc://example/media", Info: &event.FileInfo{MimeType: "image/gif"}}
	content := deletedMessageContent(original, time.Unix(0, 0), time.Unix(245, 0))
	if content.Body != "Deleted message (4min, 5sec)\n\n<test>.gif" || content.FormattedBody != "<h4>Deleted message (4min, 5sec)</h4><blockquote>&lt;test&gt;.gif</blockquote>" {
		t.Fatalf("unexpected content: %+v", content)
	}
	if content.URL != original.URL || content.Info != original.Info || content.MsgType != original.MsgType {
		t.Fatal("media was not preserved")
	}
	if original.Body != "<test>.gif" {
		t.Fatal("mutated original")
	}
	original.Format, original.FormattedBody = event.FormatHTML, "<b>hello</b>"
	if got := deletedMessageContent(original, time.Unix(0, 0), time.Unix(245, 0)).FormattedBody; got != "<h4>Deleted message (4min, 5sec)</h4><blockquote><b>hello</b></blockquote>" {
		t.Fatal(got)
	}
}

func TestWebPEmbedUpdateKeepsMedia(t *testing.T) {
	url := "https://gif.fxtwitter.com/tweet_video/example.webp"
	msg := &discordgo.Message{Content: url, Embeds: []*discordgo.MessageEmbed{{URL: url, Type: discordgo.EmbedTypeImage, Thumbnail: &discordgo.MessageEmbedThumbnail{ProxyURL: url}}}}
	part := &database.Message{AttachmentID: "video_" + url}
	if removed := removedDiscordParts(msg, []*database.Message{part}); len(removed) != 0 {
		t.Fatal("WebP embed would be redacted")
	}
	msg.Embeds = []*discordgo.MessageEmbed{}
	if removed := removedDiscordParts(msg, []*database.Message{part}); len(removed) != 1 {
		t.Fatal("genuinely removed embed was retained")
	}
}

func TestDeletionDelay(t *testing.T) {
	for _, tc := range []struct {
		delay time.Duration
		want  string
	}{
		{-time.Second, "0sec"}, {0, "0sec"}, {245 * time.Second, "4min, 5sec"}, {time.Hour + time.Second, "1hr, 1sec"}, {24*time.Hour + time.Minute, "1day, 1min, 0sec"},
	} {
		if got := formatDeletionDelay(tc.delay); got != tc.want {
			t.Errorf("got %s, want %s", got, tc.want)
		}
	}
}

func TestStickerDownloadURL(t *testing.T) {
	if got := discordStickerURL("123456789012345678", discordgo.StickerFormatTypeGIF); got != "https://media.discordapp.net/stickers/123456789012345678.gif" {
		t.Fatal(got)
	}
	if got := discordStickerURL("123", discordgo.StickerFormatTypeAPNG); got != discordgo.EndpointStickerImage("123", discordgo.StickerFormatTypeAPNG) {
		t.Fatal(got)
	}
}

func TestOriginalWebPEmbedURL(t *testing.T) {
	original := "https://gif.fxtwitter.com/tweet_video/example.webp?x=1"
	embed := &discordgo.MessageEmbed{URL: original, Thumbnail: &discordgo.MessageEmbedThumbnail{ProxyURL: "https://proxy.example/static.png"}}
	if got := originalWebPEmbedURL(embed); got != original {
		t.Fatal(got)
	}
	embed.URL = "https://example.com/post"
	embed.Thumbnail.URL = original
	if got := originalWebPEmbedURL(embed); got != original {
		t.Fatal(got)
	}
	embed.Thumbnail.URL = "file:///tmp/example.webp"
	if got := originalWebPEmbedURL(embed); got != "" {
		t.Fatal(got)
	}
}

func TestAnimatedWebPDetection(t *testing.T) {
	riff := func(chunk string, body []byte) []byte {
		data := []byte("RIFF\x00\x00\x00\x00WEBP" + chunk + "\x00\x00\x00\x00")
		binary.LittleEndian.PutUint32(data[16:20], uint32(len(body)))
		data = append(data, body...)
		if len(body)%2 != 0 {
			data = append(data, 0)
		}
		binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
		return data
	}
	animated := riff("ANIM", make([]byte, 6))
	static := riff("VP8 ", []byte("ANIM fake bytes"))
	for _, tc := range []struct {
		data []byte
		want bool
	}{
		{animated, true}, {static, false}, {animated[:len(animated)-1], false}, {[]byte("ANIM"), false}, {riff("ANIM", []byte{0}), false},
	} {
		if got := isAnimatedWebP(tc.data); got != tc.want {
			t.Fatalf("animation detection got %v want %v", got, tc.want)
		}
	}
	br := &DiscordBridge{}
	got, _, err := br.convertWebPGIF(static)
	if err != nil || !bytes.Equal(got, static) {
		t.Fatalf("static WebP was changed: %v", err)
	}
}

func TestWebPAnimationConversion(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick not installed")
	}
	dir := t.TempDir()
	animatedPath, staticPath := filepath.Join(dir, "animated.webp"), filepath.Join(dir, "static.webp")
	for _, args := range [][]string{
		{"-delay", "10", "-size", "64x64", "xc:red", "xc:blue", "-loop", "0", animatedPath},
		{"-size", "64x64", "xc:red", staticPath},
	} {
		if output, err := exec.Command("magick", args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture generation: %v: %s", err, output)
		}
	}
	br := &DiscordBridge{}
	if err := json.Unmarshal([]byte(`{"m.upload.size":10485760}`), &br.MediaConfig); err != nil {
		t.Fatal(err)
	}
	if br.MediaConfig.UploadSize == 0 {
		t.Fatal("upload limit not initialized")
	}
	animated, err := os.ReadFile(animatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !isAnimatedWebP(animated) {
		t.Fatal("real animated WebP not detected")
	}
	converted, mime, err := br.convertWebPGIF(animated)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := gif.DecodeAll(bytes.NewReader(converted))
	if err != nil || mime != "image/gif" || len(frames.Image) != 2 {
		t.Fatalf("lost animation: mime=%s err=%v", mime, err)
	}
	if frames.Delay[0] != 10 || frames.Delay[1] != 10 || frames.LoopCount != 0 {
		t.Fatalf("lost timing or loop: %+v", frames)
	}
	static, err := os.ReadFile(staticPath)
	if err != nil {
		t.Fatal(err)
	}
	if isAnimatedWebP(static) {
		t.Fatal("static WebP classified as animated")
	}
	converted, mime, err = br.convertWebPGIF(static)
	if err != nil || mime != "image/webp" || !bytes.Equal(converted, static) {
		t.Fatalf("static WebP changed: mime=%s err=%v", mime, err)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not installed")
	}
	stickerGIF := new(bytes.Buffer)
	if err := gif.EncodeAll(stickerGIF, frames); err != nil {
		t.Fatal(err)
	}
	pngData, mime, err := br.convertGIFSticker(stickerGIF.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(pngData, []byte("acTL"))
	if mime != "image/png" || at < 0 || at+8 > len(pngData) || binary.BigEndian.Uint32(pngData[at+4:at+8]) != 2 {
		t.Fatal("sticker PNG lost animation")
	}
}

func TestWebPThumbnailFallback(t *testing.T) {
	original := "https://example.com/animation.webp"
	proxy := "https://images-ext-1.discordapp.net/external/thumbnail"
	embed := &discordgo.MessageEmbed{Thumbnail: &discordgo.MessageEmbedThumbnail{ProxyURL: proxy}}
	if got := webPThumbnailFallback(embed, original); got != proxy {
		t.Fatal(got)
	}
	embed.Thumbnail.ProxyURL = original
	if got := webPThumbnailFallback(embed, original); got != "" {
		t.Fatal("must not retry failed original as fallback")
	}
	embed.Thumbnail = nil
	if got := webPThumbnailFallback(embed, original); got != "" {
		t.Fatal(got)
	}
}

func TestDiscordAttachmentSpoiler(t *testing.T) {
	portal := &Portal{Portal: &database.Portal{}, bridge: &DiscordBridge{DMA: &DirectMediaAPI{
		attachmentCache: make(map[AttachmentCacheKey]AttachmentCacheValue),
	}}}
	portal.Key.ChannelID = "1"
	portal.bridge.DMA.cfg.ServerName = "media.example"
	for _, tc := range []struct {
		filename, description string
		spoiler               bool
	}{
		{"SPOILER_image.png", "", true},
		{"SPOILER_image.png", "A caption", true},
		{"image.png", "SPOILER_ in the caption", false},
	} {
		t.Run(tc.filename+tc.description, func(t *testing.T) {
			part := portal.convertDiscordAttachment(context.Background(), nil, "2", &discordgo.MessageAttachment{
				ID: "3", Filename: tc.filename, Description: tc.description, ContentType: "image/png",
				URL: "https://cdn.discordapp.com/attachments/1/3/image.png",
			})
			raw, err := json.Marshal(&event.Content{Parsed: part.Content, Raw: part.Extra})
			if err != nil {
				t.Fatal(err)
			}
			var content map[string]any
			if err := json.Unmarshal(raw, &content); err != nil {
				t.Fatal(err)
			}
			if (content[mediaSpoilerKey] == true) != tc.spoiler {
				t.Fatalf("wrong spoiler metadata: %s", raw)
			}
			if content["msgtype"] != "m.image" || content["url"] == "" {
				t.Fatalf("lost image: %s", raw)
			}
			if tc.description != "" && content["body"] != tc.description {
				t.Fatalf("lost caption: %s", raw)
			}
		})
	}
}

func TestDeletedMediaSpoilerEdit(t *testing.T) {
	originalRaw := map[string]any{mediaSpoilerKey: true, mediaSpoilerKey + ".reason": "Sensitive", "unrelated": true}
	original := &event.MessageEventContent{MsgType: event.MsgImage, Body: "SPOILER_image.png", URL: "mxc://example/image"}
	content := deletedMessageContent(original, time.Unix(0, 0), time.Unix(5, 0))
	content.SetEdit("$original")
	raw, err := json.Marshal(&event.Content{Parsed: content, Raw: spoilerEditExtra(originalRaw)})
	if err != nil {
		t.Fatal(err)
	}
	var edit map[string]any
	if err := json.Unmarshal(raw, &edit); err != nil {
		t.Fatal(err)
	}
	updated := edit["m.new_content"].(map[string]any)
	for _, level := range []map[string]any{edit, updated} {
		if level[mediaSpoilerKey] != true || level[mediaSpoilerKey+".reason"] != "Sensitive" {
			t.Fatalf("spoiler lost: %s", raw)
		}
		if level["unrelated"] != nil {
			t.Fatalf("unrelated metadata copied: %s", raw)
		}
	}
	if updated["url"] != "mxc://example/image" || updated["msgtype"] != "m.image" {
		t.Fatalf("image lost: %s", raw)
	}
	if len(originalRaw) != 3 || original.Body != "SPOILER_image.png" {
		t.Fatal("original was mutated")
	}
	for _, plain := range []map[string]any{nil, {}, {mediaSpoilerKey: false}} {
		if spoilerEditExtra(plain) != nil {
			t.Fatal("plain media marked as spoiler")
		}
	}
}
