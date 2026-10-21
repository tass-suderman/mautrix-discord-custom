package main

import (
	"encoding/json"
	"testing"

	"github.com/bwmarrin/discordgo"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestConfigureReplyMentions(t *testing.T) {
	for _, tc := range []struct {
		name             string
		mentions         *event.Mentions
		override         any
		noReply, webhook bool
		want             *bool
	}{
		{name: "legacy default"},
		{name: "empty mentions", mentions: &event.Mentions{}, want: boolPtr(false)},
		{name: "other mention", mentions: &event.Mentions{UserIDs: []id.UserID{"@other:example.org"}}, want: boolPtr(false)},
		{name: "author mentioned", mentions: &event.Mentions{UserIDs: []id.UserID{"@author:example.org"}}},
		{name: "explicit silent", override: false, want: boolPtr(false)},
		{name: "override author mention", mentions: &event.Mentions{UserIDs: []id.UserID{"@author:example.org"}}, override: false, want: boolPtr(false)},
		{name: "explicit ping", mentions: &event.Mentions{}, override: true, want: boolPtr(true)},
		{name: "invalid override", override: "false"},
		{name: "non reply", override: false, noReply: true},
		{name: "webhook", override: false, webhook: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &discordgo.MessageSend{}
			if !tc.noReply {
				req.Reference = &discordgo.MessageReference{MessageID: "123"}
			}
			original := &discordgo.MessageAllowedMentions{Users: []string{"456"}, RepliedUser: true}
			req.AllowedMentions = original
			configureReplyMentions(req, &event.MessageEventContent{Mentions: tc.mentions}, map[string]any{replyPingKey: tc.override}, "@author:example.org", tc.webhook)
			if tc.webhook {
				if req.AllowedMentions != original {
					t.Fatal("webhook mention restrictions changed")
				}
				return
			}
			if tc.want == nil {
				if req.AllowedMentions != nil {
					t.Fatal("unexpected allowed_mentions")
				}
				return
			}
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				AllowedMentions struct {
					RepliedUser *bool    `json:"replied_user"`
					Parse       []string `json:"parse"`
				} `json:"allowed_mentions"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.AllowedMentions.RepliedUser == nil || *wire.AllowedMentions.RepliedUser != *tc.want {
				t.Fatalf("wrong reply ping on wire: %s", data)
			}
			if len(wire.AllowedMentions.Parse) != 3 {
				t.Fatalf("normal mentions disabled: %s", data)
			}
		})
	}
}

func boolPtr(value bool) *bool { return &value }
