package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

var guildRoleEvent = event.Type{Type: "fi.mau.discord.member_role", Class: event.StateEventType}
var guildRoleTrackingEvent = event.Type{Type: "fi.mau.discord.role_tracking", Class: event.StateEventType}

type guildRoleContent struct {
	RoleID string `json:"role_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Color  string `json:"color,omitempty"`
}

func highestColoredRole(guildID string, member *discordgo.Member, roles []*discordgo.Role) guildRoleContent {
	assigned := map[string]bool{guildID: true}
	for _, roleID := range member.Roles {
		assigned[roleID] = true
	}
	var best *discordgo.Role
	for _, role := range roles {
		if role == nil || role.Color == 0 || !assigned[role.ID] {
			continue
		}
		roleID, _ := strconv.ParseUint(role.ID, 10, 64)
		bestID := uint64(0)
		if best != nil {
			bestID, _ = strconv.ParseUint(best.ID, 10, 64)
		}
		if best == nil || (best.ID == guildID && role.ID != guildID) || (role.ID != guildID && (role.Position > best.Position || role.Position == best.Position && roleID < bestID)) {
			best = role
		}
	}
	if best == nil {
		return guildRoleContent{}
	}
	return guildRoleContent{best.ID, best.Name, fmt.Sprintf("#%06x", best.Color&0xffffff)}
}

func (guild *Guild) publishRoleLocked(member *discordgo.Member) {
	roles := guild.bridge.DB.Role.GetAll(guild.ID)
	discordRoles := make([]*discordgo.Role, len(roles))
	for i, role := range roles {
		discordRoles[i] = &role.Role
	}
	content := highestColoredRole(guild.ID, member, discordRoles)
	if old, ok := guild.roleSent[member.User.ID]; ok && old == content {
		return
	}
	mxid := guild.bridge.GetPuppetByID(member.User.ID).MXID
	if _, err := guild.bridge.Bot.SendStateEvent(guild.MXID, guildRoleEvent, mxid.String(), content); err != nil {
		guild.log.Warnln("Failed to publish guild role:", err)
	} else {
		guild.roleSent[member.User.ID] = content
	}
}

func (guild *Guild) updateRoleMember(member *discordgo.Member) {
	if member == nil || member.User == nil {
		return
	}
	guild.roleLock.Lock()
	defer guild.roleLock.Unlock()
	if !guild.TrackRoles || guild.MXID == "" {
		return
	}
	if guild.roleMembers == nil {
		guild.roleMembers = make(map[string]*discordgo.Member)
		guild.roleSent = make(map[string]guildRoleContent)
	}
	guild.roleMembers[member.User.ID] = member
	guild.publishRoleLocked(member)
}

func (guild *Guild) removeRoleMember(userID string) {
	guild.roleLock.Lock()
	defer guild.roleLock.Unlock()
	if !guild.TrackRoles || guild.MXID == "" {
		return
	}
	mxid := guild.bridge.GetPuppetByID(userID).MXID
	if _, err := guild.bridge.Bot.SendStateEvent(guild.MXID, guildRoleEvent, mxid.String(), guildRoleContent{}); err != nil {
		guild.log.Warnln("Failed to clear guild role:", err)
	}
	delete(guild.roleMembers, userID)
	delete(guild.roleSent, userID)
}

func (user *User) refreshTrackedRoles(guildID string) {
	guild := user.bridge.GetGuildByID(guildID, false)
	if guild == nil {
		return
	}
	guild.roleLock.Lock()
	defer guild.roleLock.Unlock()
	if !guild.TrackRoles {
		return
	}
	for _, member := range guild.roleMembers {
		guild.publishRoleLocked(member)
	}
}

type roleMemberRequest struct {
	nonce    string
	members  map[string]*discordgo.Member
	chunks   map[int]bool
	count    int
	done     chan struct{}
	complete bool
}

func (request *roleMemberRequest) add(chunk *discordgo.GuildMembersChunk) {
	if request.complete || chunk.Nonce != request.nonce || chunk.ChunkCount <= 0 || chunk.ChunkIndex < 0 || chunk.ChunkIndex >= chunk.ChunkCount {
		return
	}
	if request.count != 0 && request.count != chunk.ChunkCount {
		return
	}
	request.count = chunk.ChunkCount
	request.chunks[chunk.ChunkIndex] = true
	for _, member := range chunk.Members {
		if member != nil && member.User != nil {
			request.members[member.User.ID] = member
		}
	}
	if len(request.chunks) == request.count {
		request.complete = true
		close(request.done)
	}
}

func (guild *Guild) receiveRoleChunk(chunk *discordgo.GuildMembersChunk) {
	guild.roleLock.Lock()
	defer guild.roleLock.Unlock()
	if guild.roleRequest != nil {
		guild.roleRequest.add(chunk)
	}
}

func (guild *Guild) gatewayRoleMembers(session *discordgo.Session) ([]*discordgo.Member, error) {
	request := &roleMemberRequest{nonce: generateNonce(), members: make(map[string]*discordgo.Member), chunks: make(map[int]bool), done: make(chan struct{})}
	guild.roleLock.Lock()
	guild.roleRequest = request
	guild.roleLock.Unlock()
	defer func() { guild.roleLock.Lock(); guild.roleRequest = nil; guild.roleLock.Unlock() }()
	if err := session.RequestGuildMembers(guild.ID, "", 0, request.nonce, false); err != nil {
		return nil, fmt.Errorf("request guild members via gateway: %w", err)
	}
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	select {
	case <-request.done:
		guild.roleLock.Lock()
		defer guild.roleLock.Unlock()
		members := make([]*discordgo.Member, 0, len(request.members))
		for _, member := range request.members {
			members = append(members, member)
		}
		return members, nil
	case <-timer.C:
		return nil, fmt.Errorf("gateway member request timed out; Discord may not allow this account to enumerate all guild members (existing role metadata retained)")
	}
}

func (guild *Guild) syncRoles(user *User) error {
	session := user.Session
	if session == nil {
		return fmt.Errorf("Discord is disconnected")
	}
	var roles []*discordgo.Role
	var err error
	if session.IsUser {
		meta, stateErr := session.State.Guild(guild.ID)
		if stateErr != nil {
			return fmt.Errorf("get gateway guild roles: %w", stateErr)
		}
		session.State.RLock()
		roles = append(roles, meta.Roles...)
		session.State.RUnlock()
		if len(roles) == 0 {
			return fmt.Errorf("guild roles have not arrived from Discord yet")
		}
	} else {
		roles, err = session.GuildRoles(guild.ID)
		if err != nil {
			return fmt.Errorf("fetch guild roles: %w", err)
		}
	}
	user.handleGuildRoles(guild.ID, roles)
	state, err := guild.bridge.Bot.State(guild.MXID)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	after := ""
	var gatewayMembers []*discordgo.Member
	if session.IsUser {
		gatewayMembers, err = guild.gatewayRoleMembers(session)
		if err != nil {
			return err
		}
	}
	for {
		members := gatewayMembers
		if !session.IsUser {
			members, err = session.GuildMembers(guild.ID, after, 1000)
			if err != nil {
				return fmt.Errorf("list guild members: %w", err)
			}
		}
		for _, member := range members {
			if member.User == nil {
				continue
			}
			seen[member.User.ID] = true
			guild.updateRoleMember(member)
		}
		if session.IsUser || len(members) < 1000 {
			break
		}
		next := members[len(members)-1].User.ID
		if next == after {
			return fmt.Errorf("member pagination did not advance")
		}
		after = next
	}
	guild.roleLock.Lock()
	var removed []string
	for mxid := range state[guildRoleEvent] {
		if userID, ok := guild.bridge.ParsePuppetMXID(id.UserID(mxid)); ok && !seen[userID] {
			removed = append(removed, userID)
		}
	}
	for userID := range guild.roleMembers {
		if !seen[userID] {
			removed = append(removed, userID)
		}
	}
	guild.roleLock.Unlock()
	for _, userID := range removed {
		guild.removeRoleMember(userID)
	}
	return nil
}

func (guild *Guild) startRoleLoop(user *User) {
	guild.roleLock.Lock()
	if !guild.TrackRoles || guild.MXID == "" {
		guild.roleLock.Unlock()
		return
	}
	if guild.roleRunning {
		select {
		case guild.roleWake <- struct{}{}:
		default:
		}
		guild.roleLock.Unlock()
		return
	}
	guild.roleWake = make(chan struct{}, 1)
	guild.roleRunning = true
	guild.roleLock.Unlock()
	go func() {
		defer func() { guild.roleLock.Lock(); guild.roleRunning = false; guild.roleLock.Unlock() }()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			guild.roleLock.Lock()
			enabled := guild.TrackRoles
			guild.roleLock.Unlock()
			if !enabled || user.Session == nil {
				return
			}
			if err := guild.syncRoles(user); err != nil {
				guild.log.Warnln("Failed to sync guild roles (will retry hourly):", err)
			}
			select {
			case <-ticker.C:
			case <-guild.roleWake:
			}
		}
	}()
}

func fnGuildRoles(ce *WrappedCommandEvent) {
	if len(ce.Args) != 2 || (ce.Args[1] != "start" && ce.Args[1] != "stop") {
		ce.Reply("**Usage**: `$cmdprefix guilds roles <guild ID> <start/stop>`")
		return
	}
	guild := ce.Bridge.GetGuildByID(ce.Args[0], false)
	if guild == nil || guild.MXID == "" {
		ce.Reply("Bridge the guild first with `$cmdprefix guilds bridge <guild ID>`")
		return
	}
	if _, err := ce.User.Session.State.Guild(guild.ID); err != nil {
		ce.Reply("You must be a member of that Discord guild")
		return
	}
	enabled := ce.Args[1] == "start"
	guild.roleLock.Lock()
	_, err := ce.Bridge.Bot.SendStateEvent(guild.MXID, guildRoleTrackingEvent, "", map[string]bool{"enabled": enabled})
	if err == nil {
		guild.TrackRoles = enabled
		guild.Update()
		guild.roleSent = make(map[string]guildRoleContent)
	}
	guild.roleLock.Unlock()
	if err != nil {
		ce.Reply("Failed to change role tracking: %v", err)
		return
	}
	if enabled {
		guild.startRoleLoop(ce.User)
		ce.Reply("Role tracking enabled. Syncing all guild members; failures are logged and retried hourly.")
	} else {
		ce.Reply("Role tracking stopped. Clients should ignore previously published roles.")
	}
}
