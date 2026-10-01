package main

import (
	"encoding/json"
	"strings"

	"github.com/bwmarrin/discordgo"
	"maunium.net/go/mautrix/event"
)

func discordPresence(presence *discordgo.Presence) (event.Presence, string) {
	status := event.PresenceOffline
	// Streaming and mobile users also have the aggregate status "online".
	if presence.Status == discordgo.StatusOnline {
		status = event.PresenceOnline
	} else if presence.Status == discordgo.StatusIdle || presence.Status == discordgo.StatusDoNotDisturb {
		status = event.PresenceUnavailable
	}
	var message string
	for _, activity := range presence.Activities {
		if activity == nil || activity.Type != discordgo.ActivityTypeCustom {
			continue
		}
		message = activity.State
		if activity.Emoji.Name != "" {
			emoji := activity.Emoji.Name
			if activity.Emoji.ID != "" {
				emoji = ":" + emoji + ":"
			}
			message = strings.TrimSpace(emoji + " " + message)
		}
		break
	}
	return status, message
}

func (user *User) presenceHandler(presence *discordgo.Presence) {
	if !user.bridge.Config.Bridge.SyncPresence || presence == nil || presence.User == nil || presence.User.ID == "" {
		return
	}
	puppet := user.bridge.GetPuppetByID(presence.User.ID)
	puppet.presenceLock.Lock()
	defer puppet.presenceLock.Unlock()
	status, message := discordPresence(presence)
	intent := puppet.DefaultIntent()
	if err := intent.EnsureRegistered(); err != nil {
		puppet.log.Warn().Err(err).Msg("Failed to register ghost for presence update")
		return
	}
	// Always include status_msg, even when empty, to clear removed statuses.
	// Use the ghost rather than the double puppet to avoid changing the Matrix
	// user's own presence on other clients.
	_, err := intent.MakeRequest("PUT", intent.BuildClientURL("v3", "presence", puppet.MXID, "status"), map[string]any{
		"presence":   status,
		"status_msg": message,
	}, nil)
	if err != nil {
		puppet.log.Warn().Err(err).Msg("Failed to forward Discord presence")
	}
}

func (user *User) presencesHandler(presences []*discordgo.Presence) {
	for _, presence := range presences {
		user.presenceHandler(presence)
	}
}

func (user *User) presenceEventHandler(evt *discordgo.Event) {
	if !user.bridge.Config.Bridge.SyncPresence || evt.Type != "READY_SUPPLEMENTAL" {
		return
	}
	// The pinned discordgo version doesn't expose merged_presences in its
	// ReadySupplemental type. Decode the raw event to get initial friend and
	// guild presence for user logins.
	presences, err := decodeSupplementalPresences(evt.RawData)
	if err != nil {
		user.log.Warn().Err(err).Msg("Failed to decode initial Discord presences")
		return
	}
	user.presencesHandler(presences)
}

type supplementalPresence struct {
	discordgo.Presence
	UserID string `json:"user_id"`
}

func decodeSupplementalPresences(raw json.RawMessage) ([]*discordgo.Presence, error) {
	var data struct {
		MergedPresences struct {
			Friends []*supplementalPresence   `json:"friends"`
			Guilds  [][]*supplementalPresence `json:"guilds"`
		} `json:"merged_presences"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	all := data.MergedPresences.Friends
	for _, presences := range data.MergedPresences.Guilds {
		all = append(all, presences...)
	}
	result := make([]*discordgo.Presence, 0, len(all))
	for _, presence := range all {
		if presence == nil {
			continue
		}
		// Supplemental events can use user_id instead of user.id.
		if presence.User == nil && presence.UserID != "" {
			presence.User = &discordgo.User{ID: presence.UserID}
		}
		result = append(result, &presence.Presence)
	}
	return result, nil
}
