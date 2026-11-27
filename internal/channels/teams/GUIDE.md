Connects a **Teams bot** to **one conversation**: a personal chat with the bot, a group chat, or a team's channel. The bot is an Azure Bot you create in your own tenant.

Teams delivers messages by calling Silo, so **Silo must be reachable from the internet over https** (`public_url`), at the one address shown on this channel after you save it. That address believes only requests signed by Microsoft for this bot; nothing else gets in through it, and the rest of Silo can stay private.

### 1. Create the Azure Bot

In the [Azure portal](https://portal.azure.com), create an **Azure Bot** resource. For **Type of App** choose **Single Tenant**, and let it create a new Microsoft App ID.

Open the bot's **Configuration** and copy:

- **Microsoft App ID** into **Microsoft App ID** below,
- **App Tenant ID** into **Tenant ID** below.

Then press **Manage Password** beside the App ID, add a **New client secret**, and copy its **Value** (not its Secret ID) into **Client secret** below. The value is shown once.

### 2. Give Azure the messaging endpoint

Save this channel. It then shows its **messaging endpoint**, an `https://…` address. Paste it into the Azure Bot's **Configuration → Messaging endpoint** and apply.

### 3. Switch on Teams

In the Azure Bot, open **Channels** and add **Microsoft Teams**.

### 4. Install the bot in Teams

A bot gets into Teams as an app. In the [Teams Developer Portal](https://dev.teams.microsoft.com) create an app, and under **App features → Bot** enter the Microsoft App ID as an existing bot, with the scopes you want: **Personal**, **Team**, **Group chat**. Then **Publish → Download the app package** and upload it in Teams (**Apps → Manage your apps → Upload an app**), or publish it to your organisation. Your Teams administrator may have to allow custom apps.

Add the app where it should talk: open it for a personal chat, or add it to a team or a group chat.

### 5. Pick the conversation

A bot cannot list its conversations, so **write to it first**: a message in the personal chat, or an @mention of it in the channel or group chat. Then open **Set up** here and press **Pick a conversation**.

The bot talks only in that one conversation; add another channel for another one.

### Settings

- **Reply to** — a personal chat always gets an answer. In a team's channel or a group chat this picks between answering only when the bot is @mentioned and answering every message. Teams itself sends a bot only the messages that mention it, unless the app's manifest asks for all of them (resource-specific consent; see Microsoft's page on receiving all messages).

In a team's channel, answers go in the thread of the message they answer.

### What it cannot do

- **Files:** the bot can put small pictures (PNG, JPEG or GIF, up to 256 KB) in a message. Any other file needs Teams' own consent step and is not sent; the conversation is told there was one. Files people send the bot in a personal chat are read while Microsoft still accepts a client secret for them (it has announced an end to that in 2027).
- **History:** Teams gives a bot none, so the Bot knows what it has seen since it was connected.
- **Mentions:** the bot never notifies anyone, whatever it writes.
