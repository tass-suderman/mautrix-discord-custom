package main

import (
	"github.com/bwmarrin/discordgo"
	"testing"
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
