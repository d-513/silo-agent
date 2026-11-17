Connects a **Slack app** in your workspace to **one conversation**: a channel, a group DM, or a DM. It uses Socket Mode, so Silo dials Slack and nothing has to reach Silo from the internet. It answers as soon as someone writes, and it can read the conversation's history, threads included.

### 1. Create the app from this manifest

At [api.slack.com/apps](https://api.slack.com/apps) choose **Create New App → From a manifest**, pick your workspace, and paste this (rename it first if you like: the name is what people see):

```yaml
display_information:
  name: Silo
features:
  app_home:
    home_tab_enabled: false
    messages_tab_enabled: true
    messages_tab_read_only_enabled: false
  bot_user:
    display_name: Silo
    always_online: true
oauth_config:
  scopes:
    bot:
      - app_mentions:read
      - channels:history
      - channels:read
      - chat:write
      - files:read
      - files:write
      - groups:history
      - groups:read
      - im:history
      - im:read
      - mpim:history
      - mpim:read
      - reactions:write
      - users:read
settings:
  event_subscriptions:
    bot_events:
      - app_mention
      - message.channels
      - message.groups
      - message.im
      - message.mpim
  interactivity:
    is_enabled: false
  org_deploy_enabled: false
  socket_mode_enabled: true
  token_rotation_enabled: false
```

### 2. Make the app-level token

In the app's **Basic Information**, under **App-Level Tokens**, press **Generate Token and Scopes**, add the `connections:write` scope, and generate. Copy the `xapp-…` token into **App-level token** below.

### 3. Install it and copy the bot token

Open **Install App** (or **OAuth & Permissions**) and install the app to your workspace. Copy the **Bot User OAuth Token**, `xoxb-…`, into **Bot token** below. If you change the scopes later, reinstall the app.

### 4. Pick the conversation

Invite the app where it should talk: in a channel, type `/invite @Silo` (its name). For a DM, open the app in Slack and write to it from its **Messages** tab.

Save this channel, open **Set up**, and press **Pick a conversation**: it lists the channels and DMs the app is in. You can also paste a channel id, a `<#C…>` reference, or a link copied with **Copy link** in Slack.

The app talks only in that one conversation; add another channel for another one.

### Settings

- **Bot token** and **App-level token** — both required.
- **Reply to** — DMs always get an answer. In a channel this picks between answering only when the app is @mentioned and answering every message. A thread it answered in counts as talking to it, so a follow-up there needs no mention.
- **Answer in** — a thread under the message (the default), or the channel itself. A message written in a thread is always answered in that thread.
- **While working** — mark the message the Bot is working on with 👀 until it is done (Slack shows apps no typing indicator), or show nothing.

The app never pings `@here`, `@channel` or `@everyone`, whatever it writes.
