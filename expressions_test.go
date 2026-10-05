package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwmarrin/discordgo"
	"go.mau.fi/mautrix-discord/config"
	"go.mau.fi/mautrix-discord/database"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/event"
)

func expressionTestPortal() *Portal {
	return &Portal{Portal: &database.Portal{GuildID: "99"}, expressionPack: &discordExpressionPack{
		GuildID: "99", Images: map[string]discordExpression{
			"wave~123":        {URL: "mxc://example/wave", Body: "wave", DiscordID: "123", Usage: []string{"emoticon"}},
			"dance_party~456": {URL: "mxc://example/dance", Body: "dance_party", DiscordID: "456", Animated: true, Usage: []string{"emoticon"}},
			"sticker~789":     {URL: "mxc://example/sticker", Body: "Happy sticker", DiscordID: "789", Usage: []string{"sticker"}},
		},
	}}
}

func TestDiscordExpressionMessages(t *testing.T) {
	portal := expressionTestPortal()
	for _, tc := range []struct{ name, body, html, want string }{
		{"plain", "hello :wave: :dance_party:", "", "hello <:wave:123> <a:dance_party:456>"},
		{"explicit ID", ":wave~123:", "", "<:wave:123>"},
		{"unknown", ":unknown: :wave~999:", "", `:unknown: :wave\~999:`},
		{"escape text", "*hello* :wave:", "", `\*hello\* <:wave:123>`},
		{"formatted", "", "<strong>hello</strong> :wave:", "**hello** <:wave:123>"},
		{"picker", "", `<img data-mx-emoticon src="mxc://example/dance" alt=":wrong:" title="wrong">`, "<a:dance_party:456>"},
		{"image requires emoticon", "", `<img src="mxc://example/wave" alt="image">`, ""},
		{"unmapped picker", "", `<img data-mx-emoticon src="mxc://evil/unknown" alt=":unknown:">`, ":unknown:"},
		{"code", "", "<code>:wave:</code>", "`:wave:`"},
		{"code block", "", "<pre><code>:wave:</code></pre>", "```\n:wave:\n```"},
		{"URL", "https://example.com/:wave:", "", "https://example.com/:wave:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := &event.MessageEventContent{Body: tc.body}
			if tc.html != "" {
				content.Format = event.FormatHTML
				content.FormattedBody = tc.html
			}
			got, _ := portal.parseMatrixHTML(content, nil)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiscordExpressionDuplicateNames(t *testing.T) {
	portal := expressionTestPortal()
	portal.expressionPack.Images["wave~321"] = discordExpression{Body: "wave", DiscordID: "321", Usage: []string{"emoticon"}}
	for i := 0; i < 30; i++ {
		if got := portal.expressionText(":wave: :wave~123: :wave~321:"); got != ":wave: <:wave:123> <:wave:321>" {
			t.Fatal(got)
		}
	}
}

func TestDiscordExpressionStickerMapping(t *testing.T) {
	portal := expressionTestPortal()
	if got := portal.expressionStickerID("mxc://example/sticker"); got != "789" {
		t.Fatal(got)
	}
	if got := portal.expressionStickerID("mxc://example/wave"); got != "" {
		t.Fatal("emoji treated as sticker")
	}
	if got := portal.expressionStickerID("mxc://evil/sticker"); got != "" {
		t.Fatal("unknown MXC treated as sticker")
	}
	if got := portal.expressionStickerID(""); got != "" {
		t.Fatal("empty MXC treated as sticker")
	}
	data, err := json.Marshal(portal.expressionPack)
	if err != nil {
		t.Fatal(err)
	}
	portal.expressionPack = &discordExpressionPack{}
	if err = json.Unmarshal(data, portal.expressionPack); err != nil {
		t.Fatal(err)
	}
	if got := portal.expressionStickerID("mxc://example/sticker"); got != "789" {
		t.Fatal(got)
	}
	if got := portal.expressionText(":dance_party:"); got != "<a:dance_party:456>" {
		t.Fatal(got)
	}
}

func TestDiscordExpressionPackPublication(t *testing.T) {
	var stored *discordExpressionPack
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			if stored == nil {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"no pack"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(stored)
		case http.MethodPut:
			pack := &discordExpressionPack{}
			if err := json.NewDecoder(r.Body).Decode(pack); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			stored = pack
			writes++
			_, _ = w.Write([]byte(`{"event_id":"$pack"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	as := appservice.Create()
	as.HomeserverDomain = "example"
	as.Registration = &appservice.Registration{SenderLocalpart: "bridge", AppToken: "test"}
	if err := as.SetHomeserverURL(server.URL); err != nil {
		t.Fatal(err)
	}
	intent := as.NewIntentAPI("bridge")
	as.StateStore.MarkRegistered(intent.UserID)
	portal := expressionTestPortal()
	portal.expressionPack = nil
	portal.MXID = "!channel:example"
	portal.Type = discordgo.ChannelTypeGuildText
	as.StateStore.SetMembership(portal.MXID, intent.UserID, event.MembershipJoin)
	portal.bridge = &DiscordBridge{DMA: &DirectMediaAPI{cfg: config.DirectMedia{ServerName: "media.example"}}}
	portal.bridge.Bot = intent
	guild := &discordgo.Guild{ID: "99", Name: "Test server",
		Emojis:   []*discordgo.Emoji{{ID: "123", Name: "wave"}},
		Stickers: []*discordgo.Sticker{{ID: "789", Name: "Happy", FormatType: discordgo.StickerFormatTypePNG, Available: true}, {ID: "999", Name: "Unavailable", Available: false}},
	}
	if err := portal.syncExpressions(guild); err != nil {
		t.Fatal(err)
	}
	if stored == nil || len(stored.Images) != 2 || writes != 1 {
		t.Fatalf("bad pack: %+v, writes=%d", stored, writes)
	}
	if stored.GuildID != "99" || stored.Pack.DisplayName != "Test server (Discord)" {
		t.Fatal(stored)
	}
	if err := portal.syncExpressions(guild); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatal("unchanged pack was republished")
	}
	// A partial emoji update must retain stickers and remove the old emoji name.
	if err := portal.syncExpressions(&discordgo.Guild{ID: "99", Emojis: []*discordgo.Emoji{{ID: "123", Name: "renamed", Animated: true}}}); err != nil {
		t.Fatal(err)
	}
	if len(stored.Images) != 2 || stored.Images["sticker~789"].DiscordID != "789" {
		t.Fatal("partial update lost stickers")
	}
	if _, ok := stored.Images["wave~123"]; ok {
		t.Fatal("renamed emoji retained old name")
	}
	// Read the persisted room state after a simulated restart.
	portal.expressionPack = nil
	if got := portal.expressionText(":renamed:"); got != "<a:renamed:123>" {
		t.Fatal(got)
	}
	if got := portal.expressionStickerID(stored.Images["sticker~789"].URL); got != "789" {
		t.Fatal(got)
	}
	if err := portal.syncExpressions(&discordgo.Guild{ID: "99", Stickers: []*discordgo.Sticker{}}); err != nil {
		t.Fatal(err)
	}
	if len(stored.Images) != 1 || stored.Images["renamed~123"].DiscordID != "123" {
		t.Fatal("sticker removal lost emoji")
	}
	before := writes
	if err := portal.syncExpressions(&discordgo.Guild{ID: "other", Emojis: []*discordgo.Emoji{}}); err != nil {
		t.Fatal(err)
	}
	if writes != before {
		t.Fatal("another server overwrote the pack")
	}
}
