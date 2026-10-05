package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"reflect"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	xhtml "golang.org/x/net/html"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// MSC2545 packs are understood by Matrix clients with custom emoji/sticker pickers.
var discordExpressionType = event.Type{Type: "im.ponies.room_emotes", Class: event.StateEventType}

type discordExpression struct {
	URL       id.ContentURIString `json:"url"`
	Body      string              `json:"body"`
	Usage     []string            `json:"usage"`
	Info      *event.FileInfo     `json:"info,omitempty"`
	DiscordID string              `json:"fi.mau.discord.id"`
	Animated  bool                `json:"fi.mau.discord.animated,omitempty"`
}

type discordExpressionPack struct {
	Images map[string]discordExpression `json:"images"`
	Pack   struct {
		DisplayName string `json:"display_name"`
	} `json:"pack"`
	GuildID string `json:"fi.mau.discord.guild_id"`
}

func (portal *Portal) expressionStateKey() string { return "discord-" + portal.GuildID }

// The room state is the persistent mapping, including when direct media is enabled.
// Callers hold expressionLock. Failed reads are retried on the next message.
func (portal *Portal) loadExpressions() *discordExpressionPack {
	if portal.expressionPack != nil {
		return portal.expressionPack
	}
	pack := &discordExpressionPack{Images: make(map[string]discordExpression), GuildID: portal.GuildID}
	if portal.GuildID == "" || portal.MXID == "" || portal.bridge == nil {
		return pack
	}
	err := portal.MainIntent().StateEvent(portal.MXID, discordExpressionType, portal.expressionStateKey(), pack)
	if err == nil && pack.GuildID == portal.GuildID {
		portal.expressionPack = pack
		return pack
	}
	empty := &discordExpressionPack{Images: make(map[string]discordExpression), GuildID: portal.GuildID}
	if errors.Is(err, mautrix.MNotFound) {
		portal.expressionPack = empty
	}
	return empty
}

func expressionHasUsage(image discordExpression, usage string) bool {
	for _, item := range image.Usage {
		if item == usage {
			return true
		}
	}
	return false
}

func (pack *discordExpressionPack) emoji(shortcode string) *discordExpression {
	var match *discordExpression
	for key, image := range pack.Images {
		if !expressionHasUsage(image, "emoticon") {
			continue
		}
		if key == shortcode {
			copy := image
			return &copy
		}
		if image.Body == shortcode {
			if match != nil {
				return nil
			} // Ambiguous names must include their ID.
			copy := image
			match = &copy
		}
	}
	return match
}

func expressionMarkup(image discordExpression) string {
	prefix := ""
	if image.Animated {
		prefix = "a"
	}
	return fmt.Sprintf("<%s:%s:%s>", prefix, image.Body, image.DiscordID)
}

var expressionShortcode = regexp.MustCompile(`:([A-Za-z0-9_]+(?:~[0-9]+)?):`)

func (portal *Portal) expressionText(text string) string {
	portal.expressionLock.Lock()
	defer portal.expressionLock.Unlock()
	pack := portal.loadExpressions()
	var out strings.Builder
	offset := 0
	links := discordLinkRegex.FindAllStringIndex(text, -1)
	for _, loc := range expressionShortcode.FindAllStringSubmatchIndex(text, -1) {
		inLink := false
		for _, link := range links {
			if loc[0] >= link[0] && loc[0] < link[1] {
				inLink = true
				break
			}
		}
		if inLink {
			continue
		}
		image := pack.emoji(text[loc[2]:loc[3]])
		if image == nil {
			continue
		}
		out.WriteString(escapeDiscordMarkdown(text[offset:loc[0]]))
		out.WriteString(expressionMarkup(*image))
		offset = loc[1]
	}
	out.WriteString(escapeDiscordMarkdown(text[offset:]))
	return out.String()
}

// Replace only picker images with known MXCs; never trust client-supplied IDs or names.
func (portal *Portal) expressionHTML(input string) string {
	portal.expressionLock.Lock()
	defer portal.expressionLock.Unlock()
	pack := portal.loadExpressions()
	tokenizer := xhtml.NewTokenizer(strings.NewReader(input))
	var output strings.Builder
	codeDepth := 0
	for {
		typeID := tokenizer.Next()
		if typeID == xhtml.ErrorToken {
			break
		}
		raw := string(tokenizer.Raw())
		if typeID == xhtml.StartTagToken || typeID == xhtml.SelfClosingTagToken || typeID == xhtml.EndTagToken {
			token := tokenizer.Token()
			if token.Data == "code" || token.Data == "pre" {
				if typeID == xhtml.StartTagToken {
					codeDepth++
				} else if typeID == xhtml.EndTagToken && codeDepth > 0 {
					codeDepth--
				}
			}
			if token.Data == "img" && typeID != xhtml.EndTagToken && codeDepth == 0 {
				src, alt, emoticon := "", "", false
				for _, attr := range token.Attr {
					if attr.Key == "src" {
						src = attr.Val
					}
					if attr.Key == "alt" {
						alt = attr.Val
					}
					if attr.Key == "data-mx-emoticon" {
						emoticon = true
					}
				}
				if emoticon {
					raw = html.EscapeString(alt)
					for key, image := range pack.Images {
						if string(image.URL) == src && expressionHasUsage(image, "emoticon") {
							raw = html.EscapeString(":" + key + ":")
							break
						}
					}
				}
			}
		}
		output.WriteString(raw)
	}
	return output.String()
}

func (portal *Portal) expressionStickerID(url id.ContentURIString) string {
	portal.expressionLock.Lock()
	defer portal.expressionLock.Unlock()
	if url == "" {
		return ""
	}
	for _, image := range portal.loadExpressions().Images {
		if image.URL == url && expressionHasUsage(image, "sticker") {
			return image.DiscordID
		}
	}
	return ""
}

func (portal *Portal) syncExpressions(guild *discordgo.Guild) error {
	if portal.MXID == "" || portal.GuildID == "" || portal.GuildID != guild.ID || (guild.Emojis == nil && guild.Stickers == nil) {
		return nil
	}
	portal.expressionLock.Lock()
	defer portal.expressionLock.Unlock()
	oldPack := portal.loadExpressions()
	var conversionErrors []error
	pack := &discordExpressionPack{Images: make(map[string]discordExpression), GuildID: guild.ID}
	pack.Pack.DisplayName = oldPack.Pack.DisplayName
	if guild.Name != "" {
		pack.Pack.DisplayName = guild.Name + " (Discord)"
	}
	for key, image := range oldPack.Images {
		if (guild.Emojis == nil && expressionHasUsage(image, "emoticon")) || (guild.Stickers == nil && expressionHasUsage(image, "sticker")) {
			pack.Images[key] = image
		}
	}
	for _, emoji := range guild.Emojis {
		if emoji == nil || emoji.ID == "" {
			continue
		}
		mxc := portal.getEmojiMXCByDiscordID(emoji.ID, emoji.Name, emoji.Animated)
		if mxc.IsEmpty() {
			key := emoji.Name + "~" + emoji.ID
			conversionErrors = append(conversionErrors, fmt.Errorf("failed to copy emoji %s", emoji.ID))
			if image, ok := oldPack.Images[key]; ok {
				pack.Images[key] = image
			}
			continue
		}
		pack.Images[emoji.Name+"~"+emoji.ID] = discordExpression{URL: mxc.CUString(), Body: emoji.Name, Usage: []string{"emoticon"}, DiscordID: emoji.ID, Animated: emoji.Animated}
	}
	for _, sticker := range guild.Stickers {
		if sticker == nil || sticker.ID == "" || !sticker.Available {
			continue
		}
		mime := "image/png"
		meta := AttachmentMeta{AttachmentID: sticker.ID}
		switch sticker.FormatType {
		case discordgo.StickerFormatTypePNG:
		case discordgo.StickerFormatTypeAPNG:
			mime = "image/apng"
		case discordgo.StickerFormatTypeGIF:
			mime = "image/gif"
			meta.Converter = portal.bridge.convertGIFSticker
			meta.ConversionKey = "gif-sticker-apng-v1"
		case discordgo.StickerFormatTypeLottie:
			mime = "application/json"
			meta.Converter = portal.bridge.convertLottie
		default:
			continue
		}
		meta.MimeType = mime
		mxc := portal.bridge.DMA.StickerMXC(sticker.ID, sticker.FormatType)
		info := &event.FileInfo{MimeType: mime, Width: DiscordStickerSize, Height: DiscordStickerSize}
		if mxc.IsEmpty() || meta.Converter != nil {
			file, err := portal.bridge.copyAttachmentToMatrix(portal.MainIntent(), discordStickerURL(sticker.ID, sticker.FormatType), false, meta)
			if err != nil {
				key := "sticker~" + sticker.ID
				conversionErrors = append(conversionErrors, fmt.Errorf("copy sticker %s: %w", sticker.ID, err))
				if image, ok := oldPack.Images[key]; ok {
					pack.Images[key] = image
				}
				continue
			}
			mxc = file.MXC
			info.MimeType = file.MimeType
			info.Size = file.Size
			if sticker.FormatType == discordgo.StickerFormatTypeGIF {
				info.MimeType = "image/png"
			}
		}
		pack.Images["sticker~"+sticker.ID] = discordExpression{URL: mxc.CUString(), Body: sticker.Name, Usage: []string{"sticker"}, Info: info, DiscordID: sticker.ID}
	}
	if reflect.DeepEqual(oldPack, pack) {
		return errors.Join(conversionErrors...)
	}
	if _, err := portal.MainIntent().SendStateEvent(portal.MXID, discordExpressionType, portal.expressionStateKey(), pack); err != nil {
		return err
	}
	portal.expressionPack = pack
	return errors.Join(conversionErrors...)
}

func (user *User) syncGuildExpressions(guild *discordgo.Guild) {
	for _, portal := range user.bridge.GetAllPortalsInGuild(guild.ID) {
		if err := portal.syncExpressions(guild); err != nil {
			user.log.Warn().Err(err).Str("guild_id", guild.ID).Msg("Failed to sync Discord expressions")
		}
	}
}

var cmdSyncExpressions = &commands.FullHandler{
	Func: wrapCommand(fnSyncExpressions), Name: "sync-emotes",
	Help: commands.HelpMeta{Section: HelpSectionPortalManagement, Description: "Refresh this server's Matrix emoji and sticker packs."},
}

func fnSyncExpressions(ce *WrappedCommandEvent) {
	if ce.Portal == nil || ce.Portal.GuildID == "" {
		ce.Reply("Run this command in a bridged Discord server channel.")
		return
	}
	if ce.User.Session == nil {
		ce.Reply("Log in to Discord first.")
		return
	}
	ce.MarkRead()
	guild, err := ce.User.Session.Guild(ce.Portal.GuildID)
	if err != nil {
		ce.Reply("Failed to fetch server: %v", err)
		return
	}
	// Fetch stickers explicitly: some guild responses omit this list.
	var data []byte
	data, err = ce.User.Session.RequestWithBucketID("GET", discordgo.EndpointGuildStickers(guild.ID), nil, discordgo.EndpointGuildStickers(guild.ID))
	if err == nil {
		err = json.Unmarshal(data, &guild.Stickers)
	}
	if err != nil {
		ce.Reply("Failed to fetch stickers: %v", err)
		return
	}
	if guild.Emojis == nil {
		guild.Emojis = []*discordgo.Emoji{}
	}
	if guild.Stickers == nil {
		guild.Stickers = []*discordgo.Sticker{}
	}
	failures := 0
	for _, portal := range ce.Bridge.GetAllPortalsInGuild(guild.ID) {
		if err := portal.syncExpressions(guild); err != nil {
			failures++
			ce.User.log.Warn().Err(err).Msg("Failed to sync expression pack")
		}
	}
	if failures > 0 {
		ce.Reply("Pack refresh failed in %d room(s). Check the bridge logs and retry.", failures)
		return
	}
	ce.Reply("Refreshed server emoji and sticker packs. Send emoji as :name: or :name~id:, or select them in a compatible Matrix picker.")
}
