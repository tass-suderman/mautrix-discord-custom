package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bwmarrin/discordgo"
	"go.mau.fi/mautrix-discord/database"
	"maunium.net/go/mautrix/event"
)

func TestGalleryWireRoundTrip(t *testing.T) {
	caption := &ConvertedMessage{Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "Two photos", Format: event.FormatHTML, FormattedBody: "<b>Two photos</b>"}, Extra: map[string]any{"com.beeper.linkpreviews": []any{}}}
	image := &ConvertedMessage{AttachmentID: "123", Content: &event.MessageEventContent{MsgType: event.MsgImage, Body: "one.png", URL: "mxc://server/one", Info: &event.FileInfo{MimeType: "image/png", Width: 100}}, Extra: map[string]any{mediaSpoilerKey: true}}
	encrypted := &ConvertedMessage{AttachmentID: "456", Content: &event.MessageEventContent{MsgType: event.MsgImage, Body: "two.png", File: &event.EncryptedFileInfo{URL: "mxc://server/two"}}}
	parts := groupDiscordAttachments([]*ConvertedMessage{caption, image, encrypted}, false)
	if len(parts) != 1 || parts[0].AttachmentID != discordGalleryPartID {
		t.Fatalf("album split: %+v", parts)
	}
	gallery := parts[0]
	raw, err := json.Marshal(&event.Content{Parsed: gallery.Content, Raw: gallery.Extra})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["msgtype"] != "dm.filament.gallery" || wire["body"] != "Two photos" || wire["formatted_body"] != "<b>Two photos</b>" {
		t.Fatalf("wrong caption or format: %s", raw)
	}
	entries := wire["itemtypes"].([]any)
	first := entries[0].(map[string]any)
	if first["itemtype"] != "m.image" || first["msgtype"] != nil || first[mediaSpoilerKey] != true {
		t.Fatalf("wrong item format: %s", raw)
	}
	items, err := parseMatrixGallery(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Body != "one.png" || items[0].URL != "mxc://server/one" || items[0].Info.Width != 100 || !items[0].Spoiler || items[1].File.URL != "mxc://server/two" {
		t.Fatalf("lost media metadata: %+v", items)
	}
	if image.Content.MsgType != event.MsgImage || image.Content.Body != "one.png" {
		t.Fatal("mutated source image")
	}

	gallery.Content.SetEdit("$original")
	edit, err := json.Marshal(&event.Content{Parsed: gallery.Content, Raw: map[string]any{"m.new_content": gallery.Extra}})
	if err != nil {
		t.Fatal(err)
	}
	var edited map[string]any
	if err := json.Unmarshal(edit, &edited); err != nil {
		t.Fatal(err)
	}
	replacement := edited["m.new_content"].(map[string]any)
	if replacement["msgtype"] != "dm.filament.gallery" || replacement["body"] != "Two photos" || len(replacement["itemtypes"].([]any)) != 2 {
		t.Fatalf("edit lost album: %s", edit)
	}
}

func TestGalleryRejectsInvalidItems(t *testing.T) {
	valid := map[string]any{"itemtype": "m.image", "body": "photo.png", "url": "mxc://server/photo"}
	tooMany := make([]any, 11)
	for i := range tooMany {
		tooMany[i] = valid
	}
	for _, items := range []any{
		nil, []any{}, "invalid", tooMany,
		[]any{map[string]any{"itemtype": "m.text", "url": "mxc://server/photo"}},
		[]any{map[string]any{"itemtype": "m.image"}},
		[]any{map[string]any{"itemtype": "m.image", "url": "https://server/photo"}},
	} {
		if _, err := parseMatrixGallery(map[string]any{"itemtypes": items}); err == nil {
			t.Fatalf("accepted invalid gallery: %+v", items)
		}
	}
	ten := tooMany[:10]
	if _, err := parseMatrixGallery(map[string]any{"itemtypes": ten}); err != nil {
		t.Fatal(err)
	}
}

func TestGalleryEditsRetainIdentity(t *testing.T) {
	image := &ConvertedMessage{Content: &event.MessageEventContent{MsgType: event.MsgImage, Body: "one.png", URL: "mxc://server/one"}}
	parts := groupDiscordAttachments([]*ConvertedMessage{image}, true)
	if len(parts) != 1 || parts[0].Content.MsgType != matrixGalleryMsgType || parts[0].AttachmentID != discordGalleryPartID {
		t.Fatal("album changed identity after attachment removal")
	}
	if got := removedDiscordParts(&discordgo.Message{Attachments: []*discordgo.MessageAttachment{}}, []*database.Message{{AttachmentID: discordGalleryPartID}}); len(got) != 0 {
		t.Fatal("album would be redacted on attachment removal")
	}
	failed := &ConvertedMessage{Content: &event.MessageEventContent{MsgType: event.MsgNotice, Body: "Failed to bridge media"}}
	if got := groupDiscordAttachments([]*ConvertedMessage{image, failed}, true); len(got) != 2 || got[1] != failed {
		t.Fatal("failed attachment was hidden")
	}
}

func TestGalleryDeletionPreservesMedia(t *testing.T) {
	original := map[string]any{"itemtypes": []any{map[string]any{"itemtype": "m.image", "url": "mxc://server/image", mediaSpoilerKey: true}}}
	extra := spoilerEditExtra(original)
	if extra["itemtypes"] == nil || extra["m.new_content"].(map[string]any)["itemtypes"] == nil {
		t.Fatal("deletion edit lost gallery contents")
	}
}

func TestDiscordAlbumConversion(t *testing.T) {
	portal := expressionTestPortal()
	portal.Key.ChannelID = "1"
	portal.bridge = &DiscordBridge{DMA: &DirectMediaAPI{attachmentCache: make(map[AttachmentCacheKey]AttachmentCacheValue)}}
	portal.bridge.DMA.cfg.ServerName = "media.example"
	msg := &discordgo.Message{ID: "2", Content: "Album caption", Attachments: []*discordgo.MessageAttachment{
		{ID: "3", Filename: "one.png", ContentType: "image/png", URL: "https://cdn.discordapp.com/attachments/1/3/one.png", Flags: discordAttachmentFlagSpoiler},
		{ID: "4", Filename: "two.png", ContentType: "image/png", URL: "https://cdn.discordapp.com/attachments/1/4/two.png"},
	}}
	parts := portal.convertDiscordMessage(context.Background(), &Puppet{}, nil, msg)
	if len(parts) != 1 || parts[0].Content.MsgType != matrixGalleryMsgType || parts[0].Content.Body != "Album caption" {
		t.Fatalf("Discord album was split: %+v", parts)
	}
	items, err := parseMatrixGallery(parts[0].Extra)
	if err != nil || len(items) != 2 || !items[0].Spoiler || items[1].Spoiler {
		t.Fatalf("lost album media or spoilers: %+v, %v", items, err)
	}
	msg.Attachments = msg.Attachments[:1]
	parts = portal.convertDiscordMessage(context.Background(), &Puppet{}, nil, msg)
	if len(parts) != 1 || parts[0].Content.MsgType != event.MsgImage || parts[0].Content.Body != "Album caption" || parts[0].Content.FileName != "one.png" {
		t.Fatalf("single image caption was split: %+v", parts)
	}
}
