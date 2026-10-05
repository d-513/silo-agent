Connects a **Discord bot** to **one channel** (or one DM). It holds a gateway connection, so it answers as soon as someone writes, and it can read the channel's history.

### 1. Create the bot

In the [Discord Developer Portal](https://discord.com/developers/applications) choose **New Application**, then open **Bot**. Press **Reset Token** and copy it into **Bot token** below.

### 2. Turn on Message Content Intent

Still under **Bot**, scroll to **Privileged Gateway Intents** and enable **Message Content Intent**. Without it Discord hides what people write, and the bot only sees DMs and messages that @mention it (the channel says so when that is the case).

### 3. Add it to a server

Save this channel, open **Set up**, and press **Invite link**. Open the link, pick a server, and approve. The bot asks only for what it uses: view and write in channels and threads, read history, attach files, and link previews.

### 4. Pick the channel

In **Set up**, press **Pick a channel**: it lists the text channels the bot can write in. To talk in a DM, message the bot once, then pick the DM from the same list. You can also paste a channel id, a `discord.com/channels/…` link, or a `<#channel>` mention (turn on Developer Mode in Discord to copy ids).

The bot talks only to that one conversation; add another channel for another one.

### Settings

- **Bot token** — required.
- **Reply to** — DMs always get a reply. In a server this picks between answering only when the bot is @mentioned (or replied to) and answering every message.

The bot never pings `@everyone`, `@here` or roles, whatever it writes.
