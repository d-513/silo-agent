Signal has no bot API. Silo talks to **signal-cli**, an unofficial command-line
Signal client, which you run as a daemon next to Silo. The daemon holds the
account and its keys; Silo only sends and receives through it.

Use a spare number. signal-cli is not made or supported by Signal, and an
account driven by a program can be limited or banned.

## 1. Give signal-cli an account

Install signal-cli (github.com/AsamK/signal-cli) on a machine Silo can reach,
then either:

- **Link it to an existing account** as a secondary device, like Signal
  Desktop: run `signal-cli link -n Silo`, turn the `sgnl://…` address it prints
  into a QR code (`signal-cli link -n Silo | tee >(xargs -L 1 qrencode -t utf8)`),
  and scan it in the Signal app under Settings → Linked devices. The Bot then
  speaks as you, and your own chats pass through the daemon.
- **Register a number of its own**: `signal-cli -a +491701234567 register`
  (Signal usually asks for a captcha: `--captcha …`), then
  `signal-cli -a +491701234567 verify 123-456` with the code sent to it.

## 2. Run the daemon

```
signal-cli -a +491701234567 daemon --http 127.0.0.1:8080
```

Keep it running (a systemd unit or a container). Without `-a` the daemon
serves every account it has; Silo names the one below either way. Leave out
`--ignore-attachments` if the Bot should get files and voice messages.

**The daemon's HTTP port has no password.** Whoever reaches it can read and
send as the account. Let it listen only where Silo alone gets to it:
`127.0.0.1` when both run on one machine, otherwise a private network or a
firewall rule for Silo's address. Never a public interface. The daemon also
refuses a request that calls it by another host name than the one given to
`--http`, so use that same host and port here.

## 3. Fill in this form

- **Daemon address**: where the daemon listens, e.g. `http://127.0.0.1:8080`.
- **Account**: the number it is registered or linked as, `+491701234567`.
- **Reply to**: in a group, answer only when the account is @mentioned, or
  every message. A direct chat is always answered.

Save, then open **Set up** and pick the group or person this channel talks
to, or type a phone number or a group id (`signal-cli listGroups`). A person
must have written to the account, or be in its contacts, before the Bot can
be found by them; a group must already have the account as a member.

## Good to know

- Only messages that arrive while Silo is connected are answered. Signal
  keeps no history, so the Bot cannot read back older messages.
- Messages you send from your own phone on a linked account are not read as
  messages to the Bot.
- If your operator blocks connections to internal addresses
  (`outbound.block_private`), the daemon's address has to be allowed there.
