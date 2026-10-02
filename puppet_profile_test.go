package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"go.mau.fi/mautrix-discord/database"
	"maunium.net/go/mautrix/id"
)

func TestGuildMessageProfileUsesNicknameAndGlobalAvatarFallback(t *testing.T) {
	puppet := &Puppet{Puppet: &database.Puppet{
		Name: "Global name", AvatarURL: id.ContentURI{Homeserver: "example.org", FileID: "avatar"},
	}}
	part := &ConvertedMessage{}
	puppet.addMemberMeta(part, &discordgo.Message{
		GuildID: "server", Author: &discordgo.User{ID: "user"},
		Member: &discordgo.Member{Nick: "Server nickname"},
	})
	profile, ok := part.Extra["com.beeper.per_message_profile"].(map[string]any)
	if !ok || profile["displayname"] != "Server nickname" || profile["avatar_url"] != "mxc://example.org/avatar" || profile["id"] != "server_user" {
		t.Fatalf("unexpected server profile: %#v", profile)
	}
}

func TestDMMessageDoesNotGetGuildProfile(t *testing.T) {
	part := &ConvertedMessage{}
	(&Puppet{}).addMemberMeta(part, &discordgo.Message{Author: &discordgo.User{ID: "user"}})
	if part.Extra != nil {
		t.Fatalf("unexpected guild metadata in DM: %#v", part.Extra)
	}
}
