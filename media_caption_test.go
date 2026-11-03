package main

import (
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestAttachDiscordCaption(t *testing.T) {
	text := &ConvertedMessage{Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "caption", Format: event.FormatHTML, FormattedBody: "<b>caption</b>"}}
	image := &ConvertedMessage{AttachmentID: "123", Content: &event.MessageEventContent{MsgType: event.MsgImage, Body: "photo.png", URL: "mxc://example/photo"}, Extra: map[string]any{mediaSpoilerKey: true}}
	second := &ConvertedMessage{Content: &event.MessageEventContent{MsgType: event.MsgImage, Body: "second.png"}}
	parts := attachDiscordCaption([]*ConvertedMessage{text, image, second})
	if len(parts) != 2 || parts[0] != image || parts[1] != second {
		t.Fatal("caption was not merged into the first image")
	}
	if image.Content.Body != "caption" || image.Content.FileName != "photo.png" || image.Content.FormattedBody != "<b>caption</b>" || image.Content.Format != event.FormatHTML || image.Content.URL != "mxc://example/photo" || image.Extra[mediaSpoilerKey] != true {
		t.Fatalf("caption or media metadata lost: %+v", image)
	}
}

func TestAttachDiscordCaptionLeavesOtherMessages(t *testing.T) {
	for _, kind := range []event.MessageType{event.MsgFile, event.MsgVideo, event.MsgNotice} {
		parts := []*ConvertedMessage{{Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "text"}}, {Content: &event.MessageEventContent{MsgType: kind, Body: "file"}}}
		if got := attachDiscordCaption(parts); len(got) != 2 || got[0] != parts[0] {
			t.Fatalf("unexpected merge for %s", kind)
		}
	}
	if got := attachDiscordCaption(nil); len(got) != 0 {
		t.Fatal(got)
	}
}
