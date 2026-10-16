# Network filter (egress gateway): handoff

Status: design settled with the owner; build in progress. Read AGENTS.md
first. Silo is not deployed anywhere: the database can change freely,
containers can be cleaned up, and existing Bots are ignored (no migrations for
old boxes).

## Settled design

**Network layout**
- One gateway container, `silo-egress`, per container engine. In-tree Go: a
  guest binary `cmd/silo-egress` and an image `egressimage/`, modelled on the
  drive sidecar (`cmd/silo-drive`, `internal/drivehost`, `internal/app/drive`):
  a token per gateway, the gateway dials the CP over ConnectRPC
  (`EgressHost.Session`), hello first, and the CP pushes the full desired
  state (`EgressApply`) on every connect and change.
- Every Bot box and every STDIO sidecar gets its own internal network
  (`--internal`, engine DNS off), with the gateway attached to all of them
  plus the ordinary network. There is no route out except the gateway.
- Subnets are carved automatically by the CP from one hidden built-in pool
  (`10.213.0.0/16` in `/24`s; overridable in silo.yaml only). Nobody sets a
  container IP. The CP reads the gateway's address on each network back with
  inspect and passes it as the client's `--dns`.
- Identity = the network a connection arrives on (`State.Client(local)`).
- Upkeep: the CP attaches a network on create, detaches it on delete, and
  reconciles on a tick. A missed attach means no internet, never unfiltered
  internet.
- The CP link (worker → CP, bridge → CP): a plain TCP forward through the
  gateway. If the gateway is down, its clients are offline.
- Engines: Podman is on. Docker is implemented but refused until tested.
- `egress.enabled` is on by default; off means today's unfiltered network with
  an Admin warning. Existing Bots are ignored.
- Out of scope: drives, connectors the CP calls itself, and Lightpanda.

**How traffic reaches it**
- HTTP and HTTPS: proxy environment variables and Chromium's proxy flag, with
  the gateway as DNS answering with itself, listening on 80 and 443 as a
  safety net (transparent: Host header / SNI).
- Other protocols: a per-Bot switch, off by default, giving SOCKS5 with
  hostnames, an ssh `ProxyCommand` and `proxychains4`. SYSTEM.md explains it.

**Decryption and secrets**
- Everything is decrypted except the Pass-through list. Connections are
  re-originated with utls, mirroring the client's TLS hello.
- One CA per installation. The key stays in the CP and the gateway; the
  certificate goes into the box's stores and Chromium's NSS database. Admin
  has a Rotate button.
- Surrogate secrets: surrogate mode is a per-secret setting (the default) and
  needs allowed hosts. Surrogates are format-preserving, swapped out to
  allowed hosts only and swapped back in responses. Sending one anywhere else
  is blocked and raises an alert. `plain` mode stays for SSH keys and database
  passwords. STDIO sidecars get no surrogates.
- Side item: `${secrets.NAME}` references in a Bot's connectors.

**The filter: one mode, permissive**
- Blocks: threat lists (URLhaus, ThreatFox, Spamhaus DROP, Emerging Threats
  Open compromised IPs, HaGeZi TIF mini), surrogate leaks, private / loopback
  / metadata destinations, block lists.
- Flags only: exfil sinks, secret-shaped or known secret values in sends,
  high-entropy queries, new or random-looking domains, DNS tunnelling shapes,
  upload budgets, beaconing, executable downloads.
- The Bot is told: a plain `403` explaining a block; nothing for a flag.
- No Ask, Auto, decision model or ClamAV.
- Lists: Block / Trust / Pass-through, an operator floor with locks plus the
  owner's own.

**Rules**
- Operator layer: one layered engine for calls and results, with operator
  rows that owners may tighten but not loosen when locked. Operator policy
  text goes before the Bot's for both the auto-approval gate and the output
  filter.

**Audit**
- One `audit_events` table for calls (including default allows), results
  other than pass, network blocks, flags and alerts. Network traffic is
  summarised per minute in its own table. Search is `pg_trgm`; retention is 30
  days for summaries and 7 for detail.
- Owner: Bot Settings → Audit tab (timeline plus a Traffic view, Block host /
  Trust host on an entry); lists and the Other-protocols switch in Rules →
  Network; an alert badge on the Settings row and the Audit tab. In the chat a
  block needs no row of its own: the Bot gets the gateway's plain `403` as the
  result of the call that tried, so the thread shows it where it happened
  (an egress event carries no tool call, only the run it is put down to, so a
  mark on one row would be a guess).
- Admin: `/admin/audit` ("Needs attention" by default, collapsed repeats,
  filters in the URL, cursor paging, a summary strip, a Traffic view with
  hosts and counts only); gateway health and switches in Settings → Network.

## Build status

Done:
- `proto/silo/v1/egress.proto` and its generated Go/TS.
- `internal/egress`: host lists (`hosts.go`), threat feeds (`feeds.go`), flag
  signals and the exfil sink list (`signals.go`, `lists/`), the CA (`ca.go`),
  the gateway state (`state.go`), event and traffic batching (`report.go`),
  each with tests.
- The gateway proxy core: the verdict (`verdict.go`), DNS (`dns.go`), SOCKS5
  (`socks.go`), the ClientHello read and utls mirror (`tls.go`), the HTTP /
  transparent / MITM proxy (`proxy.go`), the CP-link forward (`cplink.go`),
  the gateway that wires the listeners (`gateway.go`), with tests.
- The gateway session (`session.go`), `cmd/silo-egress`, `egressimage/` and
  the Makefile targets (`build-egress`, `egress-image`).

- The CP side: config (`internal/config/egress.go`), the tables
  (`internal/db/egress.go`), the dockerx primitives (`internal/dockerx/egress.go`
  — `EgressUp`/`EnsureNetwork`/`NetworkConnect`/`NetworkAddr`/`ListClientNetworks`/
  `Runtime`), and `internal/app/egress` (one gateway, a `/24` per client carved
  from the pool, the CA, `renderApply`, `Session`/`Intercept`, `Reconcile`).
  A Bot's box and a STDIO sidecar are created on their own internal network with
  the gateway as their only way out; `ClientNetwork` hands back the gateway's
  address (DNS) and the CP-link URL, and a failure refuses to start the client
  (never an unfiltered box). Deleting a Bot or detaching a sidecar reclaims its
  network; a CP with no clients drops the gateway.

- Surrogate secrets. A `Secret` carries a `mode` (default `surrogate`),
  allowed `hosts` and its `surrogate` value; `internal/egress/surrogate.go`
  makes a format-preserving stand-in (same length and byte shapes, the prefix
  up to the first `-`/`_` kept), refused for a value under 8 bytes. `AddSecret`
  /`UpdateSecret` resolve the mode (no hosts and no mode means plain; a
  surrogate with no hosts or a too-short value is `InvalidArgument`) and
  `GetSecret` hands the box the stand-in, never the real value. `renderBot`
  sends surrogates and plain secrets to the gateway with their hosts (and
  channel secrets are no longer leaked into the plain list). `${secrets.NAME}`
  resolves on the connector path (headers, URL, STDIO env) to the real value,
  never in a connector's prompt text. The Secrets tab sets the mode and the
  hosts and edits a stored secret.

- The operator rule layer. `operator_rules` (`db.OperatorRule`, one row per
  connector.action or connector.`*`) is the installation's layer over the
  owner's. `internal/app/operator.go` looks a row up the owner's way (the
  action's, then the connector's `*`) and layers it: a **locked** row is a
  floor (the engine takes the stricter mode, by the ranks in
  `internal/security/layers.go`; an unknown value ranks as ask/check, so it can
  never loosen a lock) and an **unlocked** one is only a default the owner
  overrides either way. `SetRule` refuses a request that would loosen a locked
  row (`PermissionDenied`, naming the operator's value); `ListRules` carries
  the operator's value and the lock on each row so the owner's Rules page shows
  the lock chip and stops offering a looser mode. The operator's policy text
  (`rules.approval_policy` / `rules.output_policy`, a new silo.yaml group,
  joined before the Bot's own by `approval.JoinPolicies`) is read by the
  auto-approval gate and the output filter. Admin: `ListOperatorRules` /
  `SetOperatorRule` / `DeleteOperatorRule` (admin-only) behind Admin → Settings
  → Rules, which holds the rows editor and the two policy texts.

- The audit timeline. `audit_events` (`db.AuditEvent`, the old `audits` table
  renamed and widened) holds one line per tool call and its decision, per tool
  result the output filter did not pass, and per thing the network filter
  blocked, flagged or wants a human to look at (`attention`: a block, or a
  leak/secret/exfil reason). `egress_traffics` sums one minute of one client's
  traffic to one host (requests, blocked, flagged, bytes), a report added to
  what is already there. `internal/app/audit` records the first two as they
  happen (`Record`, called by the engine's `a.audit`) and is the gateway's
  `OnEvents`/`OnTraffic` sink, so nothing is dropped when a gateway is ahead of
  its CP; a background tick (`audit.Tick`) sweeps detail after
  `audit.detail_days` (7) and traffic after `audit.traffic_days` (30). A
  gateway event is put down to a run only when exactly one was live on that
  Bot; otherwise it is "outside a run". The searchable column set has a
  `pg_trgm` GIN index (`db.Migrate` creates the extension).

- The audit and network RPCs. `ListAudit` is gone, replaced by `ListAuditEvents`
  (filters, an attention view, trigram search, keyset paging), `AuditSummary`
  (what the badge shows), `MarkAuditRead` and `ListTraffic` (the sums, per
  host). The owner's own layer of the lists is `GetBotNetwork` (the operator's
  entries come back marked and are not removable), `AddHostPattern` /
  `RemoveHostPattern` and `SetOtherProtocols`, each applied to the gateway at
  once; admin-only `GetEgressStatus` and `RotateEgressCA` report the gateway's
  health and rotate the CA. `IngestEvents` raises `bots.audit_unseen` once per
  Bot for the lines that need a human. The gateway's health report is recorded
  as feed state, so a list that never reported is shown as such and the last
  good load time is kept.

- The UI. `AuditPane` (Needs attention | Everything | Traffic, a search box,
  "Load older", Block host / Trust host on a flagged line, a verdict chip and a
  human sentence for the actor) is shared by the Bot's new Audit tab — whose
  Settings row and narrow-strip chip carry the unread badge — and Admin's
  `/admin/audit`. Admin → Settings → Network holds the gateway switches, its
  health (addresses, last word, certificate with a rotation) and the threat
  lists. The owner's lists and the Other-protocols switch are on the Bot's
  Rules page (`rules/NetworkCard`), where an operator entry shows a lock.

The proxy environment variables the design first called for turned out to be
unneeded: the box is told the gateway's own address (`SILO_EGRESS_DNS`) and
resolves every name through it, so HTTP, HTTPS and DNS all go through the
gateway without a `HTTP_PROXY`/`HTTPS_PROXY` pair (which would also drag the H2C
CP link through the proxy).

A podman note that shaped the DNS: podman **ignores** a container's `--dns` on a
user-defined network (the container's resolver becomes the network's own gateway
address, `dns.podman`), so the box does not learn the gateway that way. The box
is instead told the gateway address in `SILO_EGRESS_DNS` and `botimage/start.sh`
writes it into `/etc/resolv.conf`; the gateway answers every name with itself
(the DNS safety net). The CP link is by IP, and no HTTP proxy environment
variables are set (a global `HTTP_PROXY` would drag the H2C CP link through the
proxy). `TestContainerEgressBoxIsIsolated` proves it against the real engine.

That closes this handoff: the gateway, the CP side, the operator's rule layer,
the secrets and the audit — storage, RPCs and the owner and admin UI — are all
in place.
