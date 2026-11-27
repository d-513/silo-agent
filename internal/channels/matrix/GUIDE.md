Connects a **bot account** on any Matrix homeserver to **one room**: a room it was invited to, or a direct chat. Silo asks the homeserver for news itself, so nothing has to reach Silo from the internet. It answers as soon as someone writes, and it can read the room's history.

**Encrypted rooms do not work.** Silo does not support Matrix end-to-end encryption, so it cannot read an encrypted room, and it refuses to post plain messages into one. Use a room with encryption off. Many clients turn encryption on for new private rooms and direct chats, and it cannot be turned off afterwards: make the room with the encryption switch off (in Element, under **Create room → Enable end-to-end encryption**), or use a public room.

### 1. Make an account for the bot

Register an ordinary account on your homeserver for the bot (for example `@silo:example.org`), the way you would for a person. Give it the name people should see. Do not use your own account: the Bot would speak as you.

### 2. Get its access token

Sign in as the bot once to get a token. From a terminal:

```sh
curl -X POST https://matrix.example.org/_matrix/client/v3/login \
  -H 'Content-Type: application/json' \
  -d '{"type":"m.login.password","identifier":{"type":"m.id.user","user":"silo"},"password":"…","initial_device_display_name":"Silo"}'
```

The answer's `access_token` goes into **Access token** below. Or sign in as the bot in Element and copy it from **Settings → Help & About → Advanced → Access Token**; then close that window without signing out, because signing out revokes the token.

**Homeserver URL** is the server the account is on, for example `https://matrix.org`. A bare server name works too.

### 3. Pick the room

Invite the bot to the room it should talk in, or start a direct chat with it. It accepts invitations by itself.

Save this channel, open **Set up**, and press **Pick a room**: it lists the rooms the bot is in. You can also enter a room id (`!abc:example.org`), an alias (`#room:example.org`) or a matrix.to link.

The bot talks only in that one room; add another channel for another one.

### Settings

- **Homeserver URL** and **Access token** — both required.
- **Reply to** — a direct chat (a room of two) always gets an answer. In a larger room this picks between answering only when the bot is mentioned and answering every message. A thread it answered in counts as talking to it, so a follow-up there needs no mention.

A message written in a thread is answered in that thread. Notices (what other bots send) are never answered. The bot mentions nobody, whatever it writes: a stray `@room` pages no one.
