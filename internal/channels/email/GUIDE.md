Gives the Bot a **conversation by email with one person**. The Bot reads a mailbox over IMAP and answers over SMTP: a mail from the address you pick starts or continues the conversation, and the Bot's answers come back as replies in the same thread.

This is not the Email connector (a tool the Bot uses to work an inbox) and not the Bot's own receive-only Mail address.

### 1. Give the Bot a mailbox of its own

Make a new mailbox or alias for the Bot. **Do not use your personal inbox:** every mail the picked address sends there is read to the Bot and marked read, and the Bot answers from that address.

### 2. Fill in the servers and the password

Your provider lists its **IMAP** (reading) and **SMTP** (sending) servers. The usual ports are 993 and 587; they are used when you leave the ports empty.

| Provider | IMAP server | SMTP server |
| --- | --- | --- |
| Gmail | `imap.gmail.com` | `smtp.gmail.com` |
| iCloud | `imap.mail.me.com` | `smtp.mail.me.com` |
| Fastmail | `imap.fastmail.com` | `smtp.fastmail.com` |

Gmail, iCloud and Fastmail do not take the account's own password here: make an **app password** in the account's security settings and paste that. The username is usually the full address. Fill in **From address** only when the Bot should write from another address of the same mailbox (an alias).

### 3. Pick who the Bot writes with

In **Set up**, **Pick a correspondent** lists the people who wrote to the mailbox lately; you can also type an address. The channel reads mail from that one address and writes only to it. Everything else in the mailbox is left alone.

### Why a mail can be ignored

- **It was not proven to come from that address.** The From line of a mail can be written by anyone. The Bot reads a mail only when the sender's domain signed it (DKIM), which the large providers do for everything they send. When a mail from the picked address is not signed, it is ignored and the channel's status says so. **Allow unverified mail** reads such mail anyway: turn it on only if you accept that anyone who knows the address can write to the Bot as that person.
- **A program wrote it.** Out-of-office replies, newsletters, mailing lists, bounces and `noreply` senders are never answered, so two auto-responders cannot write to each other forever.
- **It was already there.** Only mail that arrives after the channel started is read.

### What to expect

- Each answer is one mail, a reply in the thread of the mail it answers, in plain text and HTML. Files the Bot shares are attached.
- Attachments you send are saved for the Bot to open.
- When the Bot writes first, the subject is the first line of what it wrote.
- The Bot's mails are marked as written by a program. They are not saved to the mailbox's Sent folder unless your provider does that itself.
