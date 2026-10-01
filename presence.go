package main

import (
	"encoding/json"
	"strings"
	"time"

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
	status, message := discordPresence(presence)
	user.presenceCacheLock.Lock()
	defer user.presenceCacheLock.Unlock()
	if user.presenceCache == nil {
		user.presenceCache = make(map[string]cachedDiscordPresence)
	}
	cached := cachedDiscordPresence{status: status, message: message}
	cached.sent = user.sendDiscordPresence(presence.User.ID, cached)
	user.presenceCache[presence.User.ID] = cached
}

type cachedDiscordPresence struct {
	status  event.Presence
	message string
	sent    bool
}

func (user *User) sendDiscordPresence(userID string, presence cachedDiscordPresence) bool {
	puppet := user.bridge.GetPuppetByID(userID)
	puppet.presenceLock.Lock()
	defer puppet.presenceLock.Unlock()
	intent := puppet.DefaultIntent()
	if err := intent.EnsureRegistered(); err != nil {
		puppet.log.Warn().Err(err).Msg("Failed to register ghost for presence update")
		return false
	}
	// Always include status_msg, even when empty, to clear removed statuses.
	// Use the ghost rather than the double puppet to avoid changing the Matrix
	// user's own presence on other clients.
	_, err := intent.MakeRequest("PUT", intent.BuildClientURL("v3", "presence", puppet.MXID, "status"), map[string]any{
		"presence":   presence.status,
		"status_msg": presence.message,
	}, nil)
	if err != nil {
		puppet.log.Warn().Err(err).Msg("Failed to forward Discord presence")
	}
	return err == nil
}

func (user *User) refreshDiscordPresence(session *discordgo.Session) {
	// Matrix presence expires without continued activity; Discord sends
	// changes rather than heartbeats for every online contact.
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		user.Lock()
		currentSession := user.Session
		user.Unlock()
		if currentSession != session {
			return
		}
		user.bridgeStateLock.Lock()
		disconnected := user.wasDisconnected || user.wasLoggedOut
		user.bridgeStateLock.Unlock()
		if disconnected {
			continue
		}
		user.presenceCacheLock.Lock()
		for userID, presence := range user.presenceCache {
			if presence.status == event.PresenceOffline && presence.sent {
				continue
			}
			presence.sent = user.sendDiscordPresence(userID, presence)
			user.presenceCache[userID] = presence
		}
		user.presenceCacheLock.Unlock()
	}
}

func (user *User) presencesHandler(presences []*discordgo.Presence) {
	for _, presence := range presences {
		user.presenceHandler(presence)
	}
}

func (user *User) presenceEventHandler(evt *discordgo.Event) {
	if !user.bridge.Config.Bridge.SyncPresence || (evt.Type != "READY" && evt.Type != "READY_SUPPLEMENTAL") {
		return
	}
	// The pinned discordgo version doesn't expose merged_presences in its
	// Ready or ReadySupplemental types. Decode the raw event to get initial friend and
	// guild presence for user logins.
	presences, err := decodeSupplementalPresences(evt.RawData)
	if err != nil {
		user.log.Warn().Err(err).Msg("Failed to decode initial Discord presences")
		return
	}
	user.presencesHandler(presences)
	user.log.Debug().Str("event_type", evt.Type).Int("presence_count", len(presences)).Msg("Received initial Discord presences")
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
