Connects a **Telegram bot** to **one chat**. It talks MTProto, so it can read the chat's history and stay connected, not just receive messages.

### 1. Bot token

Open [@BotFather](https://t.me/BotFather), send **/newbot**, follow the prompts, and copy the token (looks like **123456:ABC-DEF…**). This is the bot's account.

### 2. API ID and API Hash

Sign in at [my.telegram.org](https://my.telegram.org) → **API development tools** → create an application if you have none. Copy the **App api_id** (a number) and **App api_hash**.

Telegram requires _every_ MTProto client to identify the application that is connecting. So even a bot needs api_id/api_hash: the token is the account, these two are the app. They are not a personal login and do not make this a user account.

### 3. Pick the chat

Save this channel, then open **Set up**.

Telegram does not let a bot list chats it has never seen, so the picker only shows chats the bot has already been in — and it remembers them across restarts. Send the bot a message first, or enter the chat directly as an **@username**, a **t.me/…** link, or a numeric chat id.

The bot talks only to that conversation; add another channel for another chat.

### Settings

- **API ID / API Hash / Bot token** — required.
- **Reply to** — private chats always get a reply. In a group this picks between answering everything and only when mentioned.
