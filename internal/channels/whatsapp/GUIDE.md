Connects a **WhatsApp account** to **one chat**. Silo joins as a _linked device_ (the same way WhatsApp Web does), so there is no bot token to get: you scan one QR code and the account is linked.

> **Use a number you can afford to lose.** This is not WhatsApp's official Business API; it is the unofficial linked-device protocol, and WhatsApp can restrict accounts that automate it. A spare SIM or a dedicated number is safer than your main one. Keep to a single chat that you or your contacts start, and never bulk-message.

### 1. Save and link

Save this channel, open **Set up**, and a QR code appears (press **Link device** if it has expired). On your phone open WhatsApp → **Settings → Linked devices → Link a device** and scan it. The code refreshes by itself while you wait.

The session lives in Silo's database, so it survives restarts. If you remove the device from your phone, the channel says so and waits for you to link again. Deleting a running channel unlinks the device for you; if it was stopped, remove it yourself under **Linked devices** on your phone.

### 2. Pick the chat

In **Set up**, press **Pick a chat**: it lists **You (message yourself)**, your groups, and the contacts saved on your phone. Or enter a phone number with the country code (`+48 600 700 800`) or a `wa.me/…` link.

**Message yourself** is the easiest way to try it: write to your own "You" chat from your phone and the Bot answers there. Only one chat is read or written; add another channel for another one.

### What it can do

- Text in both directions, with WhatsApp formatting (Markdown is translated: `**bold**` becomes `*bold*`).
- Files the Bot presents or creates are sent as photos, videos, audio or documents.
- Images, voice notes, documents and the like that people send reach the Bot as a labelled placeholder (`[image]`, `[voice message]`) plus any caption; the file itself is not downloaded.
- WhatsApp does not let a linked device fetch old messages, so the Bot reads its own log of the conversation.
- Messages that arrived while Silo was offline are not replayed.

### Settings

- **Reply to** — direct chats always get a reply. In a group this picks between answering everything and answering only when the account is @mentioned or replied to.
