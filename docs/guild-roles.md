# Guild role names and colors

Run these commands in the Matrix bridge bot management room (using your configured command prefix):

```
!discord guilds roles <guild ID> start
!discord guilds roles <guild ID> stop
```

Bridge the guild first with `guilds bridge <guild ID>`. The caller must be logged in and belong to the guild. The setting is persisted per guild. It covers every member, including members without a portal or Matrix membership. Initial and hourly reconciliation use Discord's paginated guild members API; the Discord account must have access to list all members (bot accounts need the privileged Guild Members intent). Failures are logged and retried hourly. Discord member and role events also update metadata immediately. Restart/reconnect resumes synchronization.

Matrix has no standard membership property for Discord role names/colors. This bridge reuses its existing Discord role database and publishes custom Matrix state in the **guild space**, not the user's global profile. No Matrix permissions are derived from Discord roles.

## Consumer contract for matricechat

Find the guild space using its existing `m.bridge` event (`channel.id` is the Discord guild ID), or the portal's space parent. Subscribe to these state events in the guild space:

* `fi.mau.discord.role_tracking`, state key `""`: `{"enabled":true}`. Only display role metadata while this is true. Stop publishes false; older role events remain in state/history and must be ignored.
* `fi.mau.discord.member_role`, state key equal to the user's **ghost Matrix user ID**: `{"role_id":"123456789","name":"Moderator","color":"#12ab34"}`. IDs are strings; color is a six-digit lowercase RGB hex color. For double-puppeted senders, resolve their Discord identity to the corresponding ghost ID.

Choose the highest-position assigned role with a nonzero color, ignoring higher colorless roles. Equal positions use the smaller numeric role ID. `@everyone` is the fallback if it has a color. Both the name and color come from the selected role. No colored role, or leaving the guild, publishes `{}`: remove the displayed role/color. Full reconciliation also clears stale users after a restart. Clients must handle subsequent state updates and empty content; do not retain a previously cached color when it is cleared.

Guild metadata is visible to readers of the guild space, including names/colors for Discord members who have never spoken. Stop prevents further publication but cannot erase Matrix event history. No changes to matricechat are included here.
