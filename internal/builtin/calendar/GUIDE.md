Calendar signs in to any **CalDAV** server with an **app password**, not your normal password. The password stays on the Silo server; the Bot never sees it.

- **iCloud:** appleid.apple.com → Sign-In and Security → App-Specific Passwords. The username is your Apple ID email.
- **Fastmail:** Settings → Privacy & Security → App passwords, with CalDAV access.
- **Nextcloud:** Settings → Security → Devices & sessions → Create new app password. Server URL is `https://your.host/remote.php/dav`.
- **Yahoo:** Account Security → Generate app password.
- **Other** (Radicale, Baïkal, SOGo, Zimbra, …): pick Other and paste the CalDAV URL from your host's docs. A bare host works if it supports `/.well-known/caldav`.
- **Google Calendar** does not take app passwords over CalDAV; use the Google Calendar connector instead.

Set **Time zone** (e.g. `Europe/Warsaw`) so times without an offset mean your local time.

Listing events, reading one, and checking free/busy are allowed by default. Creating, changing, and deleting events ask you first. Change these on the Rules tab.
