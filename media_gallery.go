package main

import (
	"encoding/json"
	"fmt"

	"maunium.net/go/mautrix/event"
)

// This is the gallery format used by matricechat's pinned matrix-rust-sdk.
const matrixGalleryMsgType event.MessageType = "dm.filament.gallery"
const discordGalleryPartID = "!gallery"

type matrixGalleryItem struct {
	event.MessageEventContent
	ItemType event.MessageType `json:"itemtype"`
	Spoiler  bool              `json:"page.codeberg.everypizza.msc4193.spoiler,omitempty"`
}

func isGalleryMedia(kind event.MessageType) bool {
	switch kind {
	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		return true
	default:
		return false
	}
}

func parseMatrixGallery(raw map[string]any) ([]matrixGalleryItem, error) {
	data, err := json.Marshal(raw["itemtypes"])
	if err != nil {
		return nil, err
	}
	var items []matrixGalleryItem
	if err = json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("invalid gallery: %w", err)
	}
	// Discord accepts at most ten attachments in a single message. Reject rather
	// than splitting a gallery or silently dropping any of its files.
	if len(items) == 0 || len(items) > 10 {
		return nil, fmt.Errorf("gallery must contain 1–10 attachments")
	}
	for i := range items {
		item := &items[i]
		if !isGalleryMedia(item.ItemType) {
			return nil, fmt.Errorf("unsupported gallery item type %q", item.ItemType)
		}
		item.MsgType = item.ItemType
		uri := item.URL
		if item.File != nil {
			uri = item.File.URL
		}
		if mxc, err := uri.Parse(); err != nil || mxc.IsEmpty() {
			return nil, fmt.Errorf("gallery item %d has no valid media URL", i+1)
		}
	}
	return items, nil
}

// Called before stickers and embeds are appended, so only Discord attachments
// become gallery items. Failed transfers remain visible as separate notices.
func groupDiscordAttachments(parts []*ConvertedMessage, forceGallery bool) []*ConvertedMessage {
	var items []matrixGalleryItem
	var caption *ConvertedMessage
	for i, part := range parts {
		if i == 0 && part.Content.MsgType == event.MsgText {
			caption = part
		}
		if !isGalleryMedia(part.Content.MsgType) {
			continue
		}
		content := *part.Content
		kind := content.MsgType
		content.MsgType = ""
		items = append(items, matrixGalleryItem{MessageEventContent: content, ItemType: kind, Spoiler: part.Extra[mediaSpoilerKey] == true})
	}
	if len(items) < 2 && !forceGallery {
		return attachDiscordCaption(parts)
	}
	gallery := &ConvertedMessage{
		AttachmentID: discordGalleryPartID, Type: event.EventMessage,
		Content: &event.MessageEventContent{MsgType: matrixGalleryMsgType, Body: fmt.Sprintf("%d attachments", len(items))},
		Extra:   map[string]any{"itemtypes": items},
	}
	if caption != nil {
		gallery.Content.Body = caption.Content.Body
		gallery.Content.Format = caption.Content.Format
		gallery.Content.FormattedBody = caption.Content.FormattedBody
		for key, value := range caption.Extra {
			gallery.Extra[key] = value
		}
	}
	result := []*ConvertedMessage{gallery}
	for _, part := range parts {
		if part != caption && !isGalleryMedia(part.Content.MsgType) {
			result = append(result, part)
		}
	}
	return result
}
