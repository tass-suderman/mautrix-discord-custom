package main

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"maunium.net/go/mautrix/id"
)

func TestHighestColoredRole(t *testing.T) {
	roles := []*discordgo.Role{
		{ID: "1", Name: "everyone", Color: 0x112233},
		{ID: "2", Name: "colorless", Position: 10},
		{ID: "3", Name: "lower", Position: 1, Color: 0xabcdef},
		{ID: "5", Name: "higher", Position: 2, Color: 0xff0000},
		{ID: "4", Name: "tie", Position: 2, Color: 0x00ff00},
		{ID: "6", Name: "unassigned", Position: 99, Color: 0xffffff},
	}
	for _, tc := range []struct {
		name     string
		assigned []string
		want     guildRoleContent
	}{
		{"highest colored", []string{"2", "3", "5"}, guildRoleContent{"5", "higher", "#ff0000"}},
		{"equal position lower snowflake", []string{"5", "4"}, guildRoleContent{"4", "tie", "#00ff00"}},
		{"everyone fallback", nil, guildRoleContent{"1", "everyone", "#112233"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := highestColoredRole("1", &discordgo.Member{Roles: tc.assigned}, roles); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	roles[0].Color = 0
	if got := highestColoredRole("1", &discordgo.Member{Roles: []string{"2"}}, roles); got != (guildRoleContent{}) {
		t.Fatalf("colorless roles must clear metadata: %+v", got)
	}
}

func TestRoleMemberChunks(t *testing.T) {
	request := &roleMemberRequest{nonce: "roles", members: make(map[string]*discordgo.Member), chunks: make(map[int]bool), done: make(chan struct{})}
	request.add(&discordgo.GuildMembersChunk{Nonce: "unrelated", ChunkCount: 1})
	request.add(&discordgo.GuildMembersChunk{Nonce: "roles", ChunkIndex: 2, ChunkCount: 2})
	if len(request.chunks) != 0 {
		t.Fatal("accepted unrelated or invalid chunk")
	}
	chunk := &discordgo.GuildMembersChunk{Nonce: "roles", ChunkIndex: 1, ChunkCount: 2, Members: []*discordgo.Member{nil, {User: &discordgo.User{ID: "123"}}}}
	request.add(chunk)
	request.add(chunk)
	select {
	case <-request.done:
		t.Fatal("duplicate chunk completed request early")
	default:
	}
	request.add(&discordgo.GuildMembersChunk{Nonce: "roles", ChunkIndex: 0, ChunkCount: 3})
	if request.complete {
		t.Fatal("inconsistent chunk count completed request")
	}
	request.add(&discordgo.GuildMembersChunk{Nonce: "roles", ChunkIndex: 0, ChunkCount: 2})
	select {
	case <-request.done:
	default:
		t.Fatal("out-of-order chunks failed to complete")
	}
	if len(request.members) != 1 {
		t.Fatalf("got %d members", len(request.members))
	}
	request.add(chunk) // Must not close the completion channel twice.
}

func TestGuildRoleStateKeyAvoidsReservedUserNamespace(t *testing.T) {
	mxid := id.UserID("@discord_716794514647547974:matrix.sadbean.red")
	key := guildRoleStateKey(mxid)
	if strings.HasPrefix(key, "@") {
		t.Fatal("Matrix rejects another user's @ state key even when sent by the room creator")
	}
	if key != "user:"+mxid.String() {
		t.Fatalf("unexpected consumer state key: %q", key)
	}
	decoded, ok := strings.CutPrefix(key, "user:")
	if !ok || id.UserID(decoded) != mxid {
		t.Fatal("consumer cannot recover ghost ID")
	}
}
