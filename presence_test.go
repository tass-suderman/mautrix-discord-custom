package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"maunium.net/go/mautrix/event"
)

func TestSupplementalPresences(t *testing.T) {
	presences, err := decodeSupplementalPresences([]byte(`{"merged_presences":{"friends":[null,{"user_id":"123","status":"online","activities":[{"type":4,"state":"Hello"}]}],"guilds":[[{"user":{"id":"456"},"status":"idle"}]]}}`))
	if !assert.NoError(t, err) || !assert.Len(t, presences, 2) {
		return
	}
	assert.Equal(t, "123", presences[0].User.ID)
	status, message := discordPresence(presences[0])
	assert.Equal(t, event.PresenceOnline, status)
	assert.Equal(t, "Hello", message)
	assert.Equal(t, "456", presences[1].User.ID)
	status, _ = discordPresence(presences[1])
	assert.Equal(t, event.PresenceUnavailable, status)
	_, err = decodeSupplementalPresences([]byte(`{`))
	assert.Error(t, err)
}

func TestDiscordPresence(t *testing.T) {
	for _, tc := range []struct {
		status discordgo.Status
		want   event.Presence
	}{
		{discordgo.StatusOnline, event.PresenceOnline},
		{discordgo.StatusIdle, event.PresenceUnavailable},
		{discordgo.StatusDoNotDisturb, event.PresenceUnavailable},
		{discordgo.StatusInvisible, event.PresenceOffline},
		{discordgo.StatusOffline, event.PresenceOffline},
		{"", event.PresenceOffline},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			got, message := discordPresence(&discordgo.Presence{Status: tc.status})
			assert.Equal(t, tc.want, got)
			assert.Empty(t, message)
		})
	}
}

func TestDiscordStatusMessage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		activities []*discordgo.Activity
		want       string
	}{
		{"removed", nil, ""},
		{"streaming", []*discordgo.Activity{{Type: discordgo.ActivityTypeStreaming, Name: "Stream"}}, ""},
		{"text", []*discordgo.Activity{nil, {Type: discordgo.ActivityTypeCustom, State: "Hello"}}, "Hello"},
		{"emoji", []*discordgo.Activity{{Type: discordgo.ActivityTypeCustom, State: "Hello", Emoji: discordgo.Emoji{Name: "👋"}}}, "👋 Hello"},
		{"emoji only", []*discordgo.Activity{{Type: discordgo.ActivityTypeCustom, Emoji: discordgo.Emoji{Name: "👋"}}}, "👋"},
		{"custom emoji", []*discordgo.Activity{{Type: discordgo.ActivityTypeCustom, State: "Hello", Emoji: discordgo.Emoji{ID: "123", Name: "wave"}}}, ":wave: Hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, message := discordPresence(&discordgo.Presence{Status: discordgo.StatusOnline, Activities: tc.activities})
			assert.Equal(t, event.PresenceOnline, got)
			assert.Equal(t, tc.want, message)
		})
	}
}
