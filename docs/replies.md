# Replies without pinging

When sending a Matrix reply, clients can set `fi.mau.discord.reply_ping` in the message content:

```json
{
  "msgtype": "m.text",
  "body": "A quiet reply",
  "m.relates_to": {"m.in_reply_to": {"event_id": "$original"}},
  "m.mentions": {"user_ids": []},
  "fi.mau.discord.reply_ping": false
}
```

`false` sends a native Discord reply with `allowed_mentions.replied_user: false`.
`true` enables the reply-author mention. This boolean overrides the bridge's
existing reply-ping decision from `m.mentions`. Omit it to keep existing behavior:
with `m.mentions`, the replied-to sender must be listed in `user_ids` to ping them;
without `m.mentions`, Discord's default applies. Invalid values are ignored.

This controls only the automatic reply mention. Explicit mentions in the message
text can still ping, including a mention of the reply author. Clients should omit
that author's explicit mention when offering a quiet reply. Include `m.mentions`
as appropriate to control Matrix notifications too.

The field applies to successfully resolved native replies, including media replies
and replies within threads. Ordinary messages are unaffected. Relay webhooks use
reply embeds, which have no native reply-author ping to toggle. Incoming Discord
mentions continue to be represented through `m.mentions`.

Discord documents this field in its [Allowed Mentions Object](https://discord.com/developers/docs/resources/message#allowed-mentions-object).
