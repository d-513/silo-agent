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
  Network; an alert badge on the Settings row and the Audit tab; blocks shown
  on the chat's tool row.
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

A podman note that shaped the DNS: podman **ignores** a container's `--dns` on a
user-defined network (the container's resolver becomes the network's own gateway
address, `dns.podman`), so the box does not learn the gateway that way. The box
is instead told the gateway address in `SILO_EGRESS_DNS` and `botimage/start.sh`
writes it into `/etc/resolv.conf`; the gateway answers every name with itself
(the DNS safety net). The CP link is by IP, and no HTTP proxy environment
variables are set (a global `HTTP_PROXY` would drag the H2C CP link through the
proxy). `TestContainerEgressBoxIsIsolated` proves it against the real engine.

Next, in order:
5. The operator rule layer.
6. Audit storage, RPCs and the owner and admin UI (the gateway's `OnEvents` /
   `OnTraffic` sinks, currently nil, are filled here), and the proxy environment
   variables where they do not catch the CP link.
