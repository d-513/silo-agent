Connects a **bot account** on your Mattermost server to **one conversation**: a channel, a group message, or a direct message. Silo dials your server, so nothing has to reach Silo from outside. It answers as soon as someone writes, and it can read the conversation's history.

### 1. Make the bot account

In Mattermost, open **System Console → Integrations → Bot Accounts** and turn on **Enable Bot Account Creation**. Then go to **Integrations → Bot Accounts → Add Bot Account** (from the product menu), give it a username (the name people @mention) and create it.

Copy the **token** shown once after creating it into **Access token** below. A lost token cannot be read again: make a new one from the bot's row with **Create New Token**.

No bot accounts on your server? A **personal access token** works too (System Console → Integrations → Integration Management → Enable Personal Access Tokens, then your **Profile → Security → Personal Access Tokens**). The Bot then speaks as that account, so use a spare one, not your own.

### 2. Add it where it should talk

Add the bot to a **team** (the team's menu → **Invite People**, or **Add Members to Team**), then to the **channel**: type `/invite @its-username` there. For a direct message, just write to the bot.

### 3. Connect and pick the conversation

Enter your server's address (for example `https://chat.example.com`) and the token, and save. Open **Set up** and press **Pick a channel**: it lists the channels, group messages and direct messages the bot is in. You can also paste a channel id (**View Info** in the channel's menu shows it).

The bot talks only in that one conversation; add another channel for another one.

### Settings

- **Server address** and **Access token** — both required.
- **Reply to** — direct and group messages always get an answer. In a channel this picks between answering only when the bot is @mentioned and answering every message. A thread it answered in counts as talking to it, so a follow-up there needs no mention.
- **Answer in** — a thread under the message (the default), or the channel itself. A message written in a thread is always answered in that thread.

The bot never pings `@channel`, `@all` or `@here`, whatever it writes. Posts by other bots and webhooks are ignored.
