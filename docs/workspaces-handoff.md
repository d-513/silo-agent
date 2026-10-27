# Workspaces and teams: handoff

Status: plan, not started. Read AGENTS.md first.

Settled with the owner:
- **Naming:** `space` in code, "Workspace" in the UI. Changes' switch becomes
  **Files | Drives**.
- **No migration:** the database and `data/` are reset for this change.
- **Adding from the hub grants to every Bot it may go to**, not to none.
- **Secrets stay per Bot**, like rules. They are not a Workspace resource.
- **Teams are in**, with members, roles and permissions.

A few calls inside the team model are listed at the end as open decisions.
The plan states the recommended answer for each.

## What it is

A **Workspace** holds Bots, the people who work with them, and the things the
Bots share: **connectors, skills and drives**. A shared thing is defined once
in the Workspace and **granted** to the Bots that may use it.

A Bot's own things stay its own:
- its box, SOUL, memories and chats,
- its rules and secrets,
- its channels, automations, tunnels and mailbox,
- its Dashboard, Feed and Knowledge.

What this buys:
- Sign in to GitHub once, and choose which Bots get it.
- Put the bank connector in exactly one Bot.
- Keep a skill once for the whole Workspace.
- Share a Bot with a colleague without sharing your account, and share a team
  account without sharing your Bots.

The admin library stays global. "Personal" skills become the Workspace's
skills.

Bots talking to each other and OPSEC levels come later. They build on this: a
grant is where a level check will sit, and Workspace rules (phase 7) are where
a level's floor will live.

Rules for the build:
- A Bot used by one person sees nothing new: no Workspace in the prompt, no new
  tool.
- A Bot with a grant behaves exactly as an attached Bot does today. A Bot
  without one behaves as if the resource did not exist.
- A solo user (one person in the Workspace) sees no team UI: no People rows on
  Bots, no author names, no "Can manage" lines.

## The trust model

These rules decide every permission below. The UI says them where they matter
and never claims more than they give.

1. **The Bot is the unit of trust.** Everyone who can use a Bot shares what it
   holds: its memory, files, machine, mail and connections. A determined user
   can get at any of it by asking the Bot, and the plan does not pretend
   otherwise. **The wall between people is separate Bots.**
2. **Configuration is a real boundary, because the control plane enforces it.**
   Only a Bot's managers can widen what it may do: rules, secrets, grants,
   channels, automations, SOUL, and who can use it. A user who asks the Bot to
   do one of these things meets an approval that only a manager can answer
   (see Approvals).
3. **An identity is lent, never taken.** A connection or drive someone signed
   in to acts as that person. It reaches a Bot used by others only if that
   person said so (see Identity).
4. **Workspace admins administer the Workspace, not other people's Bots.** They
   see that a Bot exists, who is on it and what it is granted. They can delete
   it and revoke a shared resource from it. They cannot open it, read it, or
   add themselves to it. Their power over a Bot is destructive, not intrusive,
   like an IT admin who can wipe a laptop but not read the mail on it.
5. **Threads are tidy, not secret.** A chat is listed to its author and the
   Bot's managers. That keeps people out of each other's way. Point 1 still
   holds, and the UI says so on a shared Bot's New chat page.

## Naming

The word "workspace" is taken in the code:
- `internal/app/workspace` is the Bot's files through the worker.
- `/workspace` is the box's home, and SYSTEM.md says so to the model.
- Changes has a **Workspace | Drives** switch.
- There are about 380 Go mentions and 110 TS mentions.

So:
- **The code says `space`:** package `internal/app/space`, table `spaces`,
  column `space_id`, routes `/w/$spaceId`.
- **The UI says "Workspace"**, the word people know. The people in it are
  "People".
- **Changes' switch becomes Files | Drives.** In Polish the files side becomes
  "Pliki", since "Obszar roboczy" will mean the container.
- **The model never hears the word.**

## Roles and permissions

### Workspace roles

The code values are `owner|admin|member|guest`. A Workspace has exactly one
owner, enforced by a partial unique index.

| | Owner | Admin | Member | Guest |
|---|---|---|---|---|
| Use the Bots they are on | ✓ | ✓ | ✓ | ✓ |
| Create a Bot | ✓ | ✓ | ✓ | |
| See the hub: connectors, skills, drives | ✓ | ✓ | ✓ | |
| Add a connector or drive (acting as themselves), install a skill | ✓ | ✓ | ✓ | |
| Edit or delete a shared resource | ✓ | ✓ | if they created it | |
| See every Bot: name, people, grants | ✓ | ✓ | | |
| Delete any Bot, revoke any grant, remove anyone from a Bot | ✓ | ✓ | | |
| Invite people, change roles, remove people | ✓ | members and guests only | | |
| Workspace rules and Activity | ✓ | ✓ | | |
| Rename the Workspace, set its mark | ✓ | ✓ | | |
| Transfer ownership, delete the Workspace | ✓ | | | |
| Leave | after transferring | ✓ | ✓ | ✓ |

- **Guest** exists so a contractor or a family member can use one Bot without
  seeing the rest. A guest can only be **Can use** on a Bot, never **Can
  manage**, because managing means granting the Workspace's resources.
- **Instance admins get nothing new.** As today, an instance admin cannot open
  another user's Bot. Admin → Users shows each user's Workspaces, and Admin
  Containers groups Bots by Workspace.

### Bot roles

The table is `bot_access(bot_id, user_id, role)`, with role `manage` or `use`.
The UI calls them **Can manage** and **Can use**. A Bot always has at least one
manager. Whoever creates a Bot manages it.

| | Can use | Can manage |
|---|---|---|
| Chat: their own chats, the main chat, New chat | ✓ | ✓ |
| Read and stop other people's chats on the Bot | | ✓ (read-only: never writes in them) |
| Approvals of their own runs: Allow once, Deny | ✓ | ✓ |
| Approvals of background runs and of manager actions, and **Always** | | ✓ |
| Files, Desktop (incl. Pause), Console, Changes (incl. Restore), Mail pane | ✓ | ✓ |
| Dashboard (check items, run actions, ask bar), Feed (read, quote) | ✓ | ✓ |
| Memories and Knowledge: read and search; artifacts; private tunnels and apps | ✓ | ✓ |
| Start the machine | ✓ | ✓ |
| Stop, reset or remove the machine | | ✓ |
| Customize: grants, skills, channels, drives | | ✓ |
| Settings: General, Rules, Secrets, Tunnels, Containers, Network, Mail | | ✓ |
| Automations: create, edit, run, delete (users can read them and their logs) | | ✓ |
| Knowledge folders, delete memories, delete Feed posts, arrange the Dashboard | | ✓ |
| Public shares of artifacts, public tunnels | | ✓ |
| People: add, set the role, remove | | ✓ |
| Delete the Bot | | ✓ |

### Identity: who a connection acts as

Connections and drives record two people:
- **`created_by`**: who added the resource to the Workspace. This person, or an
  admin, may edit or delete it.
- **`lent_by`**: whose identity it carries, meaning who completed the OAuth
  sign-in or typed a credential into its headers or STDIO environment.
  - It is empty when no person's credential is in it: no auth, the operator's
    preset values, or `${secrets.NAME}`, which resolves per Bot.

A resource with a `lent_by` is either:
- **Personal** (the default): it may be granted only to Bots whose only person
  is its lender.
- **Shared**: any manager of a Bot in the Workspace may grant it, and that Bot
  then acts as the lender. Only the lender can switch it to Shared.

**The invariant:**
- A personal resource is only ever granted to a Bot whose people are exactly
  `{lent_by}`.
- One helper, `space.CheckLend`, enforces it at every point that could break it:
  - granting the resource,
  - adding a person to a Bot,
  - signing a resource in (the new lender),
  - switching a resource from Shared to Personal. This is refused while the
    resource is in Bots other people use, and the refusal names those Bots.
- `TestPersonalIdentityStaysWithItsOwner` covers all four.

A lender can always pull their identity back:
- They can revoke the resource from any Bot, managed by them or not.
- Leaving the Workspace, being removed, or being disabled ends the lending
  (see Leaving).

Secrets are per Bot, so they are not lent through the Workspace. They record
`set_by`. When a person is added to a Bot, the confirmation names the secrets
set by someone else ("Bob will be able to have Scout use the secrets Alice
set: BANK_PIN"). Only that Bot's managers can add people, so this is a
confirmation, not a refusal.

### Approvals

An approval's voters follow from its run:
- **A run started in a person's chat:** that person may answer **Allow once**
  or **Deny**. Managers may answer it too.
- **A background run** (automation, channel, mail wake, an orphan call from the
  Console or a stray process), or a subagent working for one: managers only.
- **A manager action:** managers only, whoever's chat it came from.
  - `security.Spec` gains `Manager bool`. It is set on the actions that change
    the Bot's standing configuration:
    - `automations.create|update|delete`
    - `tunnels.public`
    - `bot.soul`
    - `bot.set_proactivity`
    - `bot.set_timezone`
    - `channels.<id>` sends, because a channel speaks as whoever linked it:
      a WhatsApp channel is its manager's own account
  - Without this, a user could ask the Bot to make an automation that runs
    with nobody watching, and approve it themselves.
- **Always** changes the rule, so only a manager may vote it. `DecideApproval`
  reads a user's `always` as `allow_once`, as it already does on an Auto
  fallback.
- **`ListApprovals`** returns only the approvals the caller may answer, so the
  Needs-you count is per person.

A rule set to **Allow** still allows, whoever is chatting. A manager who sets
`bot.soul` to Allow has decided that any user's chat may edit SOUL. Rules are
the managers' policy, and they are not second-guessed.

## Data model

There is no migration: these are the tables `AutoMigrate` makes on a fresh
database.

New:
- `spaces(id, name, mark, created_at)`.
  - `mark` is the initials tile's letters, not a crest. Crests are the Bots'
    character, and color stays a signal.
- `space_members(space_id, user_id, role, invited_by, created_at)`.
  - PK `(space_id, user_id)`.
  - Partial unique index `space_owner ON space_members(space_id) WHERE role =
    'owner'`.
- `space_invites(id, space_id, role, created_by, created_at, expires_at,
  used_at, used_by)`.
  - `id` is the hash of the link's token. A link is single use and lasts
    7 days, like an account invite.
- `bot_access(bot_id, user_id, role, feed_read_at, dashboard_seen_at,
  created_at)`.
  - PK `(bot_id, user_id)`.
  - The two watermarks are per-person read state. They replace
    `feed_posts.read_at`, `dashboard_cards.read_at` and `widgets.seen_at` as
    the unread test.
- `bot_drives(bot_id, drive_id, read_only)`: the drive grant. A grant may be
  read-only even when the drive is not.
- `space_skills(space_id, name, installed_by, created_at)`: who installed a
  Workspace skill. The body stays on disk.
- `space_rules` (phase 7): the shape of `operator_rules`, plus `space_id`.

Changed:
- `bots`:
  - `user_id` is renamed to `created_by`. The rename is on purpose: every place
    that reads it as "the owner" stops compiling, and that list is the
    checklist for the access rewrite.
  - `+ space_id` (not null, indexed).
- `connectors`:
  - `+ space_id`, `+ created_by`, `+ lent_by`, `+ shared`.
  - A Workspace connection has `space_id` set and `bot_id` empty.
  - The OAuth token moves here from `BotConnector`. `ToolsJSON` and the
    auth status stay on the grant (as built: see Tools, status and rules).
- `bot_connectors` becomes the grant. It keeps what is per Bot:
  - the MCP session key,
  - the STDIO sidecar,
  - the bridge token,
  - a per-grant `status`/`detail` for problems only that Bot has (a missing
    `${secrets.NAME}`, a sidecar that will not start).
- `drives`:
  - `bot_id` → `space_id`, `+ created_by`, `+ lent_by`, `+ shared`.
  - Drafts are per Workspace.
- `secrets`: `+ set_by`. Otherwise unchanged and still per Bot.
- `chats`:
  - `+ user_id`, the author. It is empty for the Bot's own chats: channel
    conversations, mail wakes, automation and subagent logs.
  - The main chat index becomes `chats_main ON chats(bot_id, user_id) WHERE
    main`, so there is one main chat per person.
- `runs`: `+ user_id`, who started it. It is empty for a background run.
- `run_events`:
  - `+ user_id`.
  - `+ author`, the display name, frozen when the event is written, like
    `zone`. It is set on a `user` event only while the Bot has more than one
    person.
- `audit_events`:
  - `+ space_id`, `+ actor_user`.
  - New decisions for Workspace events: `joined`, `left`, `removed`,
    `role_changed`, `granted`, `revoked`, `lent`, `signed_in`, `signed_out`,
    `bot_created`, `bot_deleted`.
- `users`: unchanged.
  - A user owns at least one Workspace: one is made with the account, at
    bootstrap, on accepting an invite, and on OIDC auto-create.

## Backend, by domain

### `access`: the rewrite everything stands on

`OwnBot` is replaced by permission checks. Today the codebase has:
- 68 `OwnBot` call sites,
- 14 `OwnBotRow` call sites,
- direct `user_id` reads in `vnc.go`, `ui.go`, `tunnels/proxy.go`,
  `artifact/content.go`, `artifact/rpc.go`, `skill/skill.go`, `users.go`,
  `channel/channel.go` and `admin_containers.go`.

The new API:
```go
access.Bot(ctx, db, botID, access.Use|access.Manage|access.Administer) (*db.Bot, error)
access.BotRow[T](ctx, db, botID, id, what, need)
access.Chat(ctx, db, chatID, access.Read|access.Write) (*db.Chat, *db.Bot, error)
access.Space(ctx, db, spaceID, access.View|access.Build|access.Admin|access.Own) (*db.Space, error)
```
- `Administer` is the Workspace admin's power over a Bot they are not on. It
  is accepted only by delete, revoke and remove-a-person.
- `Chat` gives `Read` to the author and the Bot's managers. It gives `Write`
  to the author only, and never to a manager on someone else's chat: no
  writing as another person.
  - A chat with no author belongs to the Bot, and its managers may read it.
    Only Silo writes into it.
  - A subagent's log follows its lead chat.
- **Errors:**
  - `NotFound` when the caller cannot see the thing at all, so ids cannot be
    probed.
  - `PermissionDenied` with a sentence when they can see it but not do this,
    for example "Only Scout's managers can change its rules."

Every UI RPC is declared once in a table (`rpcAccess`: method → need).
`TestEveryRPCChecksAccess` walks the service descriptor:
- It **fails on a method that is not in the table**, so a new RPC cannot ship
  without a decision.
- It drives every method as each of: a stranger, a guest, a user of the Bot, a
  manager, and a Workspace admin who is not on the Bot. It checks the code
  each one gets.

The classification:
- **Use:**
  - Machine: `GetBot`, `GetContainer`, `StartBot`, `SetDesktopPaused`.
  - Memory and Knowledge: `ListMemories`, `SearchMemories`, `ListKnowledge`,
    `SearchKnowledge`, `SyncKnowledge`.
  - Feed: `ListFeed`, `MarkFeedRead`, `QuoteFeedPost`.
  - Dashboard: `ListDashboard`, `MarkDashboardRead`, `DismissDashboardCard`,
    `SubmitDashboardCard`, `RefreshWidget`, `SetWidgetState`,
    `RunWidgetAction`, `AskDashboard`.
  - Read-only lists: `ListTunnels`, `ListMail`, `GetMail`,
    `ListAutomations`, `ListModels`, `Transcribe`, `ListCommands`.
  - Changes: `ListChanges`, `ListChangeFiles`, `GetChangePatch`,
    `RestoreChange`, `ListDriveChanges`.
  - Files: `ListFiles`, `ReadFile`, `Mkdir`, `RemoveFile`, `PutFile`.
  - What the Bot has: `ListBotConnectors`, `ListBotSkills`,
    `ListBotChannels`, `ListBotPeople`.
  - Approvals: `ListApprovals` (filtered) and `DecideApproval` (the voter
    rule).
  - Artifacts in chats the caller can read: `ListArtifacts`, `GetArtifact`,
    `ReadArtifactFile`, `OpenArtifactApp`. Also `ResolveSetupOffer` in a chat
    the caller can write.
  - HTTP routes: `/vnc`, `/console`, `/mail/raw`, `/artifacts/*`, and the
    private tunnel handoff.
- **Chat** (through `access.Chat`):
  - Read: `ListChats` (filtered), `StreamRun`, `ListChatEvents`,
    `ListSubagents`, `GetSubagent`, `GetTaskboard`.
  - Write: `CreateChat`, `Send`, `RenameChat`, `SetChatModel`,
    `SetChatThinking`, `EditMessage`, `DeleteMessage`, `DivergeChat`,
    `CompactChat`, `CollectMemories`, `ClearTaskboard`.
  - Read is enough to stop or delete: `StopRun`, `StopSubagent`,
    `DeleteChat`. A manager may stop a runaway chat, but the main chat still
    cannot be deleted.
- **Manage:**
  - Machine and settings: `UpdateBot`, `StopBot`, `ResetContainer`,
    `ListBotContainers`, `RemoveBotContainers`, `DeleteBot`.
  - Content cleanup: `DeleteMemory`, `AddKnowledgeFolder`,
    `RemoveKnowledgeFolder`, `DeleteFeedPost`, `ArrangeDashboard`,
    `RemoveWidget`.
  - Tunnels and mail: `CreateTunnel`, `UpdateTunnel`, `DeleteTunnel`,
    `DeleteMail`, `UpdateMailbox`, `RotateMailbox`.
  - Automations: `CreateAutomation`, `UpdateAutomation`, `DeleteAutomation`,
    `RunAutomation`.
  - Secrets: `ListSecrets`, `AddSecret`, `UpdateSecret`, `DeleteSecret`.
  - Rules and audit: `ListRules`, `SetRule`, `ListAuditEvents` (for a Bot).
  - Network: `GetBotNetwork`, `AddHostPattern`, `RemoveHostPattern`,
    `SetOtherProtocols`.
  - Customize: `GrantConnector`, `GrantDrive`, `SetBotSkill`,
    `RefreshBotConnector`.
  - Channels: `CreateChannel`, `UpdateChannel`, `DeleteChannel`,
    `ChannelAction`.
  - Artifacts: `ShareArtifact`, `ListArtifactShares`, `DeleteArtifactShare`,
    `DeleteArtifact`.
  - People and moves: `AddBotPerson`, `SetBotRole`, `MoveBot`.
- **Manage or Administer:** `DeleteBot`, `RevokeConnector`, `RevokeDrive`,
  `RemoveBotPerson`.
  - A lender may also revoke their own resource anywhere.
  - A person may remove themselves from a Bot unless they are its last
    manager.
- **Workspace:**
  - Any user: `ListSpaces` (their memberships), `CreateSpace`.
  - **View** (any member, guests included): `GetSpace`.
  - **Build** (members and up): `ListSpaceMembers`; the hub reads
    `ListConnectors`, `ListSkills`, `ListDrives`, `ListSkillFiles` and
    `ReadSkillFile`; the adds `CreateConnector`, `InstallSkill`, `SaveDrive`
    and `SaveSkill` (SaveSkill also needs Use on the Bot); and signing in with
    `StartConnectorAuth`, `BeginDriveAuth` and `PickDriveOptions`.
  - **Creator or Admin:** `UpdateConnector`, `DeleteConnector`, `DeleteSkill`,
    `DeleteDrive`.
  - **Lender:** `SetResourceShared`, and `BrowseDrive` on a drive with a
    lender (it reads that person's files; an admin may not).
  - **Admin:**
    - `UpdateSpace`, `ListSpaceInvites`, `CreateSpaceInvite` (only the owner
      may make an admin invite), `DeleteSpaceInvite`.
    - `SetSpaceRole` and `RemoveSpaceMember` (only the owner may act on
      admins).
    - `ListSpaceRules`, `SetSpaceRule`, `ListSpaceActivity`.
  - **Own:** `TransferSpace`, `DeleteSpace`.
  - Anyone but the owner: `LeaveSpace`.
  - Signed in, with the token: `GetSpaceInvite`, `AcceptSpaceInvite`.
- **Unchanged:**
  - The account RPCs.
  - The instance-admin RPCs (`RequireAdmin`).
  - The library: `SeedConnectors`, `SeedSkills`, admin `CreateConnector` on
    the library.

Long-lived access is checked again when access is lost:
- `space.Remove` and `RemoveBotPerson` publish an access change.
- `StreamRun` ends that person's streams on the Bot.
- The hub drops their VNC and console sockets (`Hub.DropViewer(botID,
  userID)`).
- Tunnel grants and artifact views already re-check on every request. That
  check becomes "this session's user can still use the Bot".

### `space`: the new domain

- **Workspace RPCs:**
  - `ListSpaces`, `CreateSpace`, `UpdateSpace`.
  - `DeleteSpace`: owner only. It is refused while the Workspace has Bots, and
    refused for the caller's last owned Workspace.
  - `TransferSpace`: to an admin. The old owner becomes an admin.
- **Bots in a Workspace:**
  - `ListBots` takes `space_id`. It returns the Bots the caller is on, with
    their role.
  - For a Workspace admin it also returns `others`: name, crest, people and
    grant counts, and nothing that would open them.
  - `CreateBot` takes `space_id`. An empty one means the caller's first owned
    Workspace, for iOS until it catches up.
- **The invariant every grant checks:** the Bot and the resource are in the
  same Workspace.
  - One helper checks it: `space.SameSpace(botID, resource)`.
  - `TestGrantsStayInSpace` drives every grant RPC with a resource from
    another Workspace, including the caller's own other Workspace.
- **`space.CheckLend`:** see Identity.

### People: invites, roles, leaving

**Invites:**
- `CreateSpaceInvite(space, role)` returns `{public_url}/join/<token>` once,
  like an account invite. Only the hash is kept.
- `/join/$token` is a route under `_authed`. A signed-out visitor goes through
  `/signin?from=…` and comes back, the usual guard.
- **The link is a bearer**, like an account invite: whoever opens it, signed
  in, joins. Invites are never matched by email, because a user can set their
  own email with no confirmation (`email_self_set`).
- **A link for someone without an account** makes one only while the operator
  has `spaces.invite_signup` on (default off). Then the join page has the
  `AcceptInvite` fields. Otherwise it says to ask the instance admin.
- Accepting adds the person with the invite's role and an audit row, and lands
  on `/w/$spaceId`.

**Roles:**
- `SetSpaceRole` follows the Workspace table.
- Demoting someone to Guest turns their **Can manage** into **Can use**.
  - It is refused when that would leave a Bot without a manager. The refusal
    names those Bots.

**Bot people:**
- `AddBotPerson(bot, user, role)`:
  - The user must be in the Workspace.
  - A guest can only get `use`.
  - `space.CheckLend` runs. It refuses while the Bot holds someone's personal
    resource, and says which one and whose.
  - The secrets confirmation lists secrets set by others.
- `SetBotRole` and `RemoveBotPerson` keep at least one manager on the Bot.

**`space.Remove(spaceID, userID, plan)`** is the one routine behind removing a
person, leaving, and deleting or disabling a user:
1. **Resources they lent:**
   - A personal one is revoked everywhere and deleted, since their identity
     leaves with them.
   - A shared one is signed out: the token or credential is cleared, and the
     status becomes "Needs sign-in" with "Alice left". The row and its grants
     stay, so someone else can sign it in.
2. **Bots where they were the last manager** are resolved by `plan`. The UI
   asks per Bot:
   - make another person on it a manager,
   - let the remover take it over,
   - or delete it.
   - An empty `plan` with such Bots is refused, and the refusal lists them.
3. **Their access ends:**
   - `bot_access` rows are deleted.
   - Live runs from their chats are stopped.
   - The access change is published (streams, VNC, console).
   - Their open invites are deleted.
4. **Their chats stay with the Bots.** They were readable by managers before,
   and they still are.
5. An audit row is written.

**Instance lifecycle:**
- **`DeleteUser`:** for each Workspace the user owns, ownership goes to the
  oldest admin, else the oldest member. With nobody else in it, the Workspace
  is deleted with its Bots. Then `space.Remove` runs everywhere, with the
  "delete" plan for last-managed Bots.
- **Disabling a user** (`SuspendUser`) no longer stops "their Bots". It:
  - **suspends the Bots that have no enabled manager**: `StartRun` refuses
    them, their channels stop, and the tunnel proxy answers 404, as today for
    a disabled owner;
  - **suspends the resources they lent**: calls fail with "Alice's account is
    disabled". Tokens are kept, so enabling the user brings it all back.
  - A single helper answers both questions:
    `space.Suspended(botID)` / `space.LenderOK(resource)`.

### Skills

- **On disk:** `data/skills/spaces/<space>/<name>/`, kind `space`.
  `RootFor(kind, spaceID)` replaces the user key.
- **RPCs:**
  - `skillScope` takes `scope = space|library` + `space_id`.
  - Install, delete, files and read take `space_id`.
  - A `space_skills` row records who installed the skill.
- **Deleting** prunes `bot_skills` of the Workspace's Bots.
- **Bot-keyed functions** read `bot.SpaceID` where they read the user today:
  `Enabled`, `Push`, `Read`, `Peek`, `PullTree`, `Propose`, `InstallFiles`.
- **`artifact.SaveSkill`** installs into the Bot's Workspace. It needs Use on
  the Bot and Build on the Workspace, so a guest cannot install.
- **`/artifacts/skill.zip`** with scope `space` checks `access.Space(View)`.
- **New Bot and adding a skill:** enable every Workspace skill on the Bots the
  caller manages (the "default all" decision). Each one can be switched off.

### Connectors (the largest piece)

**Workspace connections:**
- `uniqueName`/`slugTaken` scan the Workspace, not the Bot.
- The connector add RPCs create in a Workspace and set `created_by`.
  - Signing in sets `lent_by`, and so does typing a credential into the
    headers or STDIO environment.
- `CreateBotConnector` is kept for "add from a Bot's page". It creates in the
  Workspace and grants to that Bot.

**Grants:**
- `GrantConnector` / `RevokeConnector`.
- `AttachConnector(bot, connector)`, which iOS uses, becomes the grant.
- `DetachConnector` becomes the revoke. It:
  - drops that Bot's MCP session and STDIO sidecar,
  - runs `PruneRules(botID, slug)` for that Bot only,
  - runs `PushTools`.
- Deleting a connection revokes every grant first.

**The OAuth token lives on the connection and is refreshed once:**
- Today each session builds its own `oc.TokenSource`, and `saveToken` writes
  the token per attachment (`oauth.go:180-215`).
- With N sessions on one token they would race to refresh. A provider that
  rotates refresh tokens would leave all but one of them dead.
- So there is one `oauth2.TokenSource` per connection, single-flight:
  - Before refreshing, it re-reads the row under a lock (`locks` key
    `"mcptok:"+connector.ID`).
  - Every session's `InitialTokenSource`/`NewTokenSource` wraps it.
- Pending sign-ins (`s.oauth`) stay bound to the user who started them.

**Isolation stays per grant:**
- **MCP sessions** stay per grant (`s.mcp` keyed by `BotConnector.ID`).
  Lightpanda gives each session its own browsing context, and two Bots must
  not share one.
- **STDIO sidecars** stay per grant: `silo-mcp-<BotConnector.ID>`, the npm
  cache per Bot, and the egress client per grant. One process serving two
  boxes would be a channel between them.

**`${secrets.NAME}` resolves per grant, from that Bot's own secrets**
(`connectorVars(botID)`, as today):
- The definition is shared and the credential is the Bot's.
- A Bot without the secret has a grant status of "Add the secret NAME to
  Scout". The connection's other grants are unaffected.
- STDIO `{name, secret}` env entries work the same way.
- Such a connection has no `lent_by`.
- `TestConnectorSecretVarsArePerBot` covers this.

**Tools, status and rules:**
- **As built, `ToolsJSON` and the auth status stay per grant**, not on the
  connection as first planned: `${secrets.NAME}` resolves per Bot, so two
  Bots' sessions on one connection can reach different accounts and list
  different tools. Each grant refreshes its own session (`refreshGrants`
  re-drives them all after a sign-in or an edit) and `PushTools` runs per
  Bot; `storeTools` prunes that Bot's rules.
- **Renaming a connection moves its rules** (`<old-slug>.*` → `<new-slug>.*`)
  for every granted Bot.
  - Today a rename loses them to `sweepRules`. That is a bug, and with sharing
    it would hit several Bots at once.

**Defaults:**
- `CreateBot` grants `defaults()` (auto_attach + `autoenable_connectors`). It
  reuses the Workspace's connection made from that preset when one exists and
  makes one otherwise.
- So two Bots with Lightpanda share one connection and still have two
  sessions.

**Unchanged and new details:**
- `sameTarget`/`keepOnlyGiven` are unchanged: they compare the connection with
  its preset.
- **A call through a lent connection names the lender when the run's person is
  someone else:**
  - the thread's call row reads "GitHub · as Alice",
  - the audit row carries it,
  - `security.Describe` adds "signed in as Alice" for the approval gate.

### Drives

- Drive RPCs take `space_id`; drafts are per Workspace.
- `GrantDrive` / `RevokeDrive` take `read_only`.
- `renderApply(botID)` joins `bot_drives`. A mount is read-only when the drive
  or the grant is.
- **The sidecar stays one per Bot** (`silo-drive-<bot>`). Credentials still
  never enter a box, and each Bot mounts only its grants.
- **Token fan-out:**
  - `saveDriveToken` writes the Workspace drive, then re-applies to every
    other granted Bot's sidecar, so a rotated refresh token reaches them.
  - A provider that kills the old refresh token at once (Box) can still race
    two sidecars refreshing together. Test this per OAuth template. If the
    race is real, mark the template `single_mount` and allow only one grant.
- **Two mounts of one remote each have a write-back cache.** The last writer
  wins, as with two laptops on one Dropbox. The UI says so where a drive is
  granted to a second Bot.
- `lent_by`/`shared` and `space.CheckLend` work exactly as for connectors.

### Secrets (staying per Bot)

- The tables, `GetSecret`, egress surrogates and the Rules section are
  unchanged.
- `set_by` is recorded for the add-person confirmation.
- Bot Settings → Secrets stays where it is, manager-only.

### Conversations with several people

All of this switches on at the first second person on a Bot. A Bot with one
person runs exactly as today, byte for byte.

**Authors:**
- `Send` and `CreateChat` set `chats.user_id`, `runs.user_id` and
  `run_events.user_id`.
- A `user` event gets `author` (the display name, frozen) while the Bot has
  more than one person.
- `replayEvents` puts the author on the stamp line it already writes from
  `zone`, so it reads `Bob · Sun, 2026-10-11 14:30 …`.
- An event written before the second person arrived has no author and replays
  as it did, so `TestHistoryIsAppendOnly` still holds.
- Add a two-person case to that test: adding a person in the middle of a chat
  must not change any earlier bytes.

**The Bot learns who its people are:**
- The session tier gains one line, written only while there is more than one
  person: "People who use you: Alice (manages you), Bob. Each message says who
  wrote it."
- A membership change rewrites the tier, which costs that Bot one cache miss,
  as a new connector does.
- `SYSTEM.md` and the prompts that say "your owner" (`ask_owner`, the pause
  refusal, proactivity, `ONBOARDING.md`) are reworded in the docs phase. The
  pause refusal names the person who holds the desktop.
- The memory collector's transcript carries the author names, so a memory
  reads "Bob prefers…".

**The main chat, one per person:**
- `chats.Main(bot, user)` finds or makes it.
- **Placements:**
  - The Feed quote, a Dashboard submit and a `hand` action land in the main
    chat of **the person who pressed**.
  - The interview lands in the creator's main chat.
  - A mail wake opens a Bot-owned chat (no author), which managers can read.
- **`ask_owner` and `offer_setup` from a background run** go to the main chat
  only while the Bot has one person, as today (`Proactive`). With several,
  they wait on the Dashboard, which everyone on the Bot sees.
- Asked in a person's chat, they stay in that chat, as today.

**What each person sees:**
- **Chats list:** your chats in the date groups, as today. For managers, a
  collapsed **Team** group follows: other people's chats with the author's
  initials, then the Bot's own chats (channels, mail).
- **Someone else's chat:** read-only. The composer is replaced by an `ink-3`
  line: "Bob's chat. You can read it and stop it; only Bob writes in it."
- **Activity strip:** links only to chats you can read. Others read "Working
  in another chat".
- **Feed and Dashboard:** shared. "Since you last looked" and the unread badge
  use your own watermark on `bot_access`.
- **Time zone:** follows the device only while the Bot has one person. With
  several it stays put, and Settings → Time zone says why.
- **Language:** unchanged. It defaults to the creator's display language.

**Tools that read across chats** (`chats`, the memory collector) are not
narrowed: the Bot is the unit of trust. The New chat page of a shared Bot says
so in one line: "Scout's managers can read this chat. What you tell Scout, it
may remember for everyone who uses it."

### Workspace rules (phase 7)

- `space_rules` sits between the operator's rules and each Bot's.
  - A **locked** row is a floor that no Bot in the Workspace may loosen.
  - An **unlocked** row is a default.
  - `security.Stricter` takes the strictest of the three layers.
- `ListRules` carries the Workspace value and lock beside the operator's, and
  the Rules page names the source: "Set by the workspace".
- The policy text `space.approval_policy`/`output_policy` is joined after the
  operator's and before the Bot's.
- This is where OPSEC levels will later live: a level is a named set of
  Workspace floors.

### Activity and audit

- Workspace events are audit rows with `space_id` and `actor_user`.
- **Workspace → Activity** (admins) lists:
  - joins, leaves and role changes,
  - Bots created and deleted,
  - grants and revokes,
  - sign-ins and sign-outs,
  - switches between Personal and Shared.
- A Bot's own audit stays with its managers. A Workspace admin sees that a
  grant happened, not what the Bot did with it.

### Cleanup

- **`dropBot`** deletes the Bot's grants and `bot_access` rows, never the
  shared rows.
- **`DeleteSpace`** (empty) deletes:
  - the connections (with their sidecars, which have no grants left by then),
  - the drives,
  - the skills directory,
  - the invites, members and rules.
- **`DeleteUser`** follows the order in People.

## Reset, not migration

Nothing is deployed, so the change lands on an empty database and an empty
`data/`. The owner runs `make db-reset` and `make reset-data`. These commands
are destructive, so whoever builds this never runs them unasked.

What the reset means for the code:
- **No backfill code**, no `personal` skill kind, no compatibility reads of
  `bots.user_id`.
- **`apptest`'s helpers** make a Workspace with its owner, then Bots in it
  with the creator as manager. `apptest.NewBot` gets a `space` option.
  `apptest.Person(t, space, role)` adds a second user for team tests.

## UI

### Routes (`router.tsx`)

- `/bots/$botId/...` is unchanged: Bot ids are global.
- New:
  - `/` redirects to the last Workspace (`silo.space` in localStorage, else
    the first).
  - `/w/$spaceId` is the Workspace home: the folio cards of the Bots you are
    on (`BotsPage`, filtered). For admins it adds an **Other Bots** section.
  - `/w/$spaceId/new` is New Bot in that Workspace. `/new` redirects to it.
  - `/w/$spaceId/customize` is a layout with tabs **Connectors**, **Skills**
    and **Drives**. Add, edit and setup pages are routes of their own:
    - `/connectors/new`
    - `/connectors/$connectorId`
    - `/drives/new/$template`
    - `/drives/$driveId`
  - `/w/$spaceId/settings` is a layout with tabs **General**, **People**,
    **Rules** (phase 7) and **Activity** (phase 7):
    - `/people/invite`
    - `/people/$userId` (role, Bots, and the remove flow)
  - `/join/$token` is the invite page.
  - `/bots/$botId/people` is a new Bot Settings tab.
  - `/skills` redirects to `/w/$last/customize/skills`.
- The current Workspace is the route's (`spaceId`, or the open Bot's
  `space_id`), else the last used.
- Every new page's `staticData.tab` is added so `data-tab-on` lights it.

### Rail (DESIGN.md §6 Rail)

- The Silo mark's slot becomes the **Workspace tile**: a 32px `well` square
  with initials in `ink` 13/600, and the `silo-mark` under it as today.
  - A Workspace with other people shows a small two-heads glyph in the tile's
    corner.
- Pressing the tile opens a `shadow-slip` menu:
  - each Workspace: tile, name, and the role when it is not Owner, as a `well`
    chip "Guest"/"Member"/"Admin". The current one has the `cobalt` bar.
  - a hairline,
  - **New workspace**,
  - **Workspace settings**.
- Below the tile:
  - **Bots** (this Workspace's home),
  - **Customize** (the blocks glyph, replacing the Skills book; hidden for
    guests),
  - the hairline,
  - the crests of the Bots you are on,
  - `+` (hidden for guests).
- Below 960 the tile leads the top bar.

### Customize hub (the Workspace's)

- It uses the Section frame of the Bot's Customize (`bot/SectionTabs.tsx`): a
  56px head, tabs, the sliding underline.
- Each card keeps its current design and gains a footer with:
  - **Used by:** up to five 20px crests, then "+N", or "No Bot uses it" in
    `ink-3`. Only Bots you are on are drawn; for an admin, the others are a
    count.
  - **Who it acts as**, for a resource with a lender: "Signed in by Alice",
    and a `well` chip **Personal** or **Shared**.
  - A member does not see other people's personal resources at all. An admin
    sees them, dimmed, for oversight.
- **Choose Bots** opens a `shadow-slip` list with a switch per Bot you manage.
  A Bot the resource may not go to has its switch disabled and the reason in
  a Tip: "Personal: Scout is also used by Bob."
- **An add flow ends with a Which Bots can use it `Step`.** Every Bot it may
  go to starts on (the "default all" decision). Coming from a Bot (`?for=`),
  that Bot is on and listed first.
- **Sign-in gets a Who can use this sign-in `Step`** when the Workspace has
  other people:
  - **Only my Bots** (default): "Bots that only you use. Nobody else can give
    it to a Bot."
  - **Anyone in Acme:** "Any Bot in the workspace may get it, and acts as you
    on GitHub." A second click arms it.
  - When the connection is already in Bots other people use, Only my Bots
    lists the Bots it would leave.
- **Connectors:**
  - The pills become **In this workspace** | **Library** | **Custom**.
  - The add form is the shared `ConnectorFields`.
  - OAuth happens here once, through the existing `ConnectorAuthSlip`, lifted
    out of `BotPage` into a slip host shared by the hub and the Bot page.
- **Skills:** the Hub's Personal | Library becomes **Workspace** | **Library**.
  The install band is unchanged. A row shows "Installed by Alice" in a shared
  Workspace.
- **Drives:** the drive list, gallery and `DriveForm`, at the Workspace level.

### A Bot's Customize: what this Bot uses

Managers only. A user of the Bot does not get the Customize or Settings rows
in the sidebar.

- **Connectors:** pills **In use** | **Workspace** | **Library**.
  - **Workspace** lists the Workspace's other connections it may get, each
    with **Use in this Bot**: a grant, with no sign-in.
  - **Library** creates in the Workspace and grants here.
  - An In use card shows a `well` chip **Also in 2 Bots** (a Tip lists them)
    and, when lent, "as Alice".
  - Its Edit form opens with one `ink-2` line: "Changes apply to every Bot
    that uses it: Scout, Ledger." It is read-only for a manager who is
    neither the creator nor an admin.
  - **Remove from this Bot** is the revoke. **Delete** lives only in the hub:
    armed, and naming the Bots that lose it.
- **Skills:** the same switches, in groups Library | Workspace.
- **Drives:** the granted drives, then **From the workspace** (Add to this
  Bot, with Read-only), then **New drive**.
- **Channels:** unchanged.

### Workspace settings

- **General:**
  - Name and mark.
  - **Leave workspace** (not for the owner).
  - **Transfer ownership** (owner): pick an admin, then a second click.
  - **Delete workspace** (owner): armed, and refused while it has Bots.
- **People** (members see the list; admins edit it):
  - Rows: a 28px initials circle in `well`, name and email, the role as a
    segmented control **Admin | Member | Guest** (Owner is a chip), the number
    of Bots they are on, and an armed **Remove**.
  - **Invite** opens `/people/invite`: pick a role, **Make link**, then the
    link is shown once with Copy (the account-invite pattern). Open invites
    are listed under it with their expiry and Delete.
  - **Remove** opens `/people/$userId`, a page rather than a slip, because it
    has choices:
    - each Bot where they are the last manager, with **Make Bob manager** |
      **Take it over** | **Delete it**;
    - what they lent: the personal resources that will be deleted and the
      shared ones that will need signing in again;
    - then **Remove Alice**, armed.
  - **Leaving** walks through the same page for yourself.
- **Rules** (phase 7): the Bot Rules page's sections and segmented controls,
  plus a lock per row and the two policy texts.
- **Activity** (phase 7): the audit timeline filtered to the Workspace's
  events.

### Joining

`/join/$token` is an `AuthCard`-style page: "Alice invited you to **Acme** as a
Member", then **Join** | **Not now**.
- With `spaces.invite_signup` on and nobody signed in, it shows the account
  fields.
- An expired or used link reads as such.

### A Bot's People (Settings → People)

- Rows: initials, name, **Can use | Can manage** (segmented, like a rule; a
  guest's row has only Can use), and an armed **Remove**.
  - Managers edit the list. Users see it read-only. Admins who are not on the
    Bot see it on the Other Bots card.
- **Add people** opens a `shadow-slip` picker of the Workspace's people who
  are not on the Bot. When adding is refused, the slip says why, in
  `vermilion`, with the way out:
  - "Alice's GitHub is personal. Alice can share it, or you can remove it from
    Scout."
  - The secrets line appears as a second click.
- **Leave this Bot**, for yourself, unless you are its last manager.

### The Bot page with several people

- **Sidebar head:** up to three initials circles and "+N" beside the status.
  A Tip lists the people and their roles.
- **New chat:** the trust line ("Scout's managers can read this chat. What
  you tell Scout, it may remember for everyone who uses it.").
- **Thread:**
  - A user bubble shows its author's initials when the Bot has several people.
  - A connector call made through someone else's sign-in reads "GitHub · as
    Alice".
- **Approval slip:** the source line names the person ("Bob's chat · Report").
  A user sees **Allow once** and **Deny** only. A manager action shows "Needs
  a manager" to a user and does not offer the vote.
- **Dashboard and Feed:** unchanged, with per-person unread.
- **Workspace home, Other Bots** (admins): dimmed cards with crest, name, the
  people's initials, the grant count and an armed **Delete**. They cannot be
  opened: no link, and the Tip "Only people on Scout can open it."

### New Bot (`/w/$spaceId/new`)

- The current form, plus a **What it can use** `Step`. It lists the
  Workspace's connectors, drives and skills that the Bot may get, all ticked
  (the "default all" decision), each one a click to untick.
- Plus a **Who can use it** `Step` when the Workspace has other people:
  - **Just me** (default), or pick people.
  - Picking someone unticks your personal resources above and says why.

### Solo looks the same

With one person in the Workspace:
- No People rows on Bots, no initials, no trust lines.
- The sign-in step has no Who can use this sign-in.
- Settings → People shows only the Invite button with a one-line
  explanation.

### iOS

- The Skill Hub sheet shows the Workspace's skills.
- Add connector becomes a grant.
- A Workspace picker sits at the sidebar's foot.
- Until then the RPC shims keep the current app working:
  - an empty `space_id` means the caller's first owned Workspace,
  - `AttachConnector` is the grant,
  - and a team action the app does not know comes back as a
    `PermissionDenied` with a sentence it already shows.

### i18n

- Every new string goes into `pl.ts`/`de.ts`; `i18n_test.ts` enforces it.
- Role names take a `context` (Member vs member of a list).

## Phases

Each phase is shippable on its own, written tests first, in one commit or a
few.

1. **Workspaces and the access layer.**
   - Backend:
     - `spaces`, `space_members` (the owner row), `bot_access` (the creator as
       manager), `bots.space_id`/`created_by`.
     - `access.Bot`/`Chat`/`Space` replacing `OwnBot` at every site.
     - The `rpcAccess` table and `TestEveryRPCChecksAccess`, with the stranger
       and owner cases for now.
     - The space RPCs, with `ListBots`/`CreateBot` scoped.
   - UI: the rail tile and menu, the `/w/$spaceId` home, New Bot in a
     Workspace, the New workspace page and Settings → General.
   - Nothing is shared yet, and one person behaves exactly as today.
2. **Skills move.**
   - Disk, kind, RPCs, `space_skills`, `SaveSkill`.
   - The hub shell with its Skills tab (replacing `/skills`), the Bot's Skills
     groups, the iOS shim.
3. **Connectors move.**
   - The connection + grant model, the shared token source, sessions and
     sidecars per grant, `${secrets.NAME}` per grant, slugs per Workspace,
     rename moves rules, defaults, revoke vs delete.
   - `created_by`/`lent_by`/`shared` are recorded. The check matters only
     once there are other people.
   - The hub's Connectors tab; the Bot's **Workspace** pill, chips and the
     warning.
4. **Drives move.**
   - `bot_drives` with read-only per grant, the sidecar render, token fan-out,
     drafts per Workspace, the rotation test per OAuth template.
   - The hub's Drives tab; the Bot's **From the workspace**.
5. **People and roles.**
   - Backend:
     - Invites and `/join`, `space_invites`, `spaces.invite_signup`.
     - Workspace roles, `bot_access` roles, Add/Set/Remove on a Bot.
     - `space.CheckLend` and Personal/Shared.
     - `space.Remove` with its plan.
     - Transfer and leave.
     - Disable and delete user, reworked (`Suspended`, `LenderOK`).
     - Access changes end streams and sockets.
     - The full `TestEveryRPCChecksAccess` matrix.
   - UI: Settings → People with the invite and remove pages, Bot → People,
     the join page, the hub's Who it acts as, the sign-in step, Choose Bots
     with refusals, Other Bots for admins, New Bot's Who can use it.
6. **Conversations with several people.**
   - Backend:
     - Authors on chats, runs and events, with the frozen `author`.
     - The session-tier people line.
     - A main chat per person, and the placements.
     - Chat read/write through `access.Chat`.
     - Approval voters and `Spec.Manager`.
     - Per-person watermarks.
     - Zone following only when solo.
     - Collector transcripts with names.
     - "as Alice" on calls.
   - UI: the Team group in the chats list, the read-only thread, author
     initials, the trust line, the slip's person and votes, the sidebar
     people.
7. **Workspace rules and activity.**
   - `space_rules` in the layers, policy texts, the Rules page's source and
     lock.
   - Workspace audit rows and Settings → Activity.
8. **Rounding off.**
   - **Moving a Bot to another Workspace** (Settings → General, manager of
     the Bot and Build in the target). The move:
     - lists what the Bot loses,
     - drops cross-Workspace grants,
     - disables skills it cannot keep,
     - removes people who are not in the target.
   - **An Access tab in the hub** (admins): resources × Bots, a switch per
     cell, refusals shown in place. This is the overview OPSEC levels will
     stand on.
   - iOS parity.
   - AGENTS.md, DESIGN.md, SYSTEM.md and the prompts' "owner" wording, and
     the e2e spec (`data-tab-on` for the new tabs).

## Tests that must exist by the end

Beyond the ones each change brings:
- **`TestEveryRPCChecksAccess`:** every method declared, and every role's code
  checked.
- **`TestGrantsStayInSpace`:** every grant RPC refuses a resource from another
  Workspace, including the caller's own other Workspace.
- **`TestUngrantedIsAbsent`:** an ungranted resource leaves no `tools.<slug>`,
  no prompt section, no mount, and no synced skill.
- **`TestRevokePrunesOnlyThatBot`.**
- **`TestRenameMovesRules`.**
- **`TestSharedTokenRefreshesOnce`:** two sessions and one expired token make
  one refresh, and both sessions use the new token.
- **`TestConnectorSecretVarsArePerBot`.**
- **`TestPersonalIdentityStaysWithItsOwner`:** grant, add person, sign in, and
  Shared → Personal.
- **`TestAdminCannotOpenOthersBots`:**
  - `NotFound` on `GetBot`, `StreamRun`, `ListFiles`, `/vnc`, `/console`, a
    private tunnel and an app view.
  - Delete and revoke work.
  - Adding themselves is refused.
- **`TestOthersChatsAreReadOnly`:** a manager reads and stops Bob's chat;
  `Send`/`EditMessage` are refused. A user cannot list or stream someone
  else's chat.
- **`TestApprovalVoters`:**
  - A user can answer their own chat's approval with once or deny.
  - A user's `always` becomes once.
  - Manager actions and background approvals are for managers only.
  - `ListApprovals` is filtered.
- **`TestRemovePersonHandsOff`:**
  - Personal resources are deleted and shared ones signed out.
  - The last-manager plan is required and applied.
  - Streams, sockets and tunnel grants end.
- **`TestDisabledLenderSuspends`** and **`TestBotWithNoEnabledManagerSuspends`.**
- **`TestInviteIsSingleUse`:** an admin cannot make an admin invite, and a
  guest is never a manager.
- **`TestReadStateIsPerPerson`.**
- **`TestAuthorsAreFrozen`.**
- **`TestHistoryIsAppendOnly`** still passes, with a two-person case:
  - a Bot's prompt does not move when something is granted to another Bot;
  - adding a person mid-chat changes no earlier bytes.

## Open decisions

The plan takes the recommended answer. Say if any is wrong:

1. **Workspace admins and other people's Bots:** they can delete and revoke
   but not open (recommended, trust model point 4). The alternative is that
   admins can open every Bot. That is simpler, but then nobody's sign-in can
   be personal, because an admin could always use it.
2. **Who sees a chat:** its author and the Bot's managers (recommended). The
   alternatives are everyone on the Bot (one team notebook) or the author
   only (managers cannot supervise).
3. **Invite links making accounts:** only while the operator turns on
   `spaces.invite_signup` (recommended, default off). The alternative is that
   a Workspace admin can always bring in a new person.
4. **The Guest role:** in (recommended). Without it, everyone who uses one Bot
   also sees the hub and can make Bots.
