# SiloOS

A bootable operating system image that runs Silo and nothing else. It is a
[bootc](https://bootc-dev.github.io/bootc/) image: an ordinary OCI image,
built with `podman build`, that a machine boots and updates from a registry as
a whole (`bootc upgrade`, with rollback). The host is Fedora's bootc base;
everything Silo is runs in containers.

## What runs

Everything runs under one unprivileged user, `silo` (uid 1000), on its own
rootless Podman, started by its user manager at boot (lingering). The units are
Quadlets in `rootfs/etc/containers/systemd/users/1000/`:

| Container | What | Where |
| --- | --- | --- |
| `silodb` | Postgres 17 + pgvector | `127.0.0.1:5432`, volume `silodb-data` |
| `silo` | The control plane with the console (`ghcr.io/d-513/silo`) | host network: 8080 (HTTP), 25 (mail), 3479 UDP/TCP (calls) |
| `silocaddy` | `silo-caddy` (`images/caddy/`): Caddy with 17 DNS providers, configured by the control plane (`caddy.*`, `internal/caddy`) | host network: 80, 443; admin API only on a socket in `/run/user/1000/silocaddy` |
| `silo-…` | The Bots' boxes and sidecars, made by the control plane through `silo`'s Podman socket | their own networks |

On the host, as root, runs **siloosd** (`cmd/siloosd`, `internal/siloosd`,
`proto/silo/v1/siloos.proto`): the one way Silo changes the machine. Admin →
SiloOS (shown only on SiloOS) talks to it through the control plane.

The control plane runs as `docs/CONFIGURATION.md` § Running the control plane
in a container says: the socket at `/run/docker.sock`, `/var/lib/silo` bound at
the same path, keep-id so its uid 1000 is the host's `silo`. It is on the host
network so it sees each client's real address, a call's WebRTC offers the
machine's own addresses, and the Bots' boxes reach it at the default `cp_url`.

**The OS is the release.** The image names one control plane (`SILO_IMAGE`,
baked in at build), and that control plane runs the bot, STDIO, drive and
egress images of its own commit. So a SiloOS version is one matched set, and
updating Silo is updating the OS.

## Layout

| Path | Holds |
| --- | --- |
| `/var/lib/silo` | The control plane's `silo.yaml` (Admin → Settings) and `data/` (labelled `container_file_t`, `rootfs/usr/share/siloos/selinux/siloos.cil`, since Bot boxes bind its folders unlabelled) |
| `/var/home/silo` | `silo`'s Podman storage: images, containers, the database's volume |
| `/etc/silo/silo.env` | The operator's `SILO_*` settings (from `rootfs/usr/share/siloos/silo.env`); they win over Admin → Settings |
| `/etc/silo/postgres.env`, `secrets.env` | The database password and the first admin, made once by `siloos-init` |
| `/run/siloosd/siloosd.sock` | siloosd's socket (`root:silo 0750` folder), bound into the control plane's container only |
| `/var/lib/siloosd` | siloosd's update policy and a network change waiting to be kept |
| `/usr/lib/siloos/release` | What the image is: its version, commit, the repository its channels are tags of, and the `silo` and `silo-caddy` it runs |
| `/etc/cloud/cloud.cfg.d/05_siloos.cfg` | cloud-init: only where a cloud answers, `admin` as the default user |
| `/usr/lib/bootc/kargs.d/10-siloos-console-*.toml` | The console on the screen and the first serial port, where the banner shows |

`siloos-init.service` (`rootfs/usr/libexec/siloos/init`) runs before `silo`'s
user manager on every boot and makes only what is missing, so nothing an
operator changed is overwritten.

## Channels

A SiloOS machine follows one tag of `ghcr.io/d-513/silo-os`, its **channel**,
as bootc does: an update is that tag fetched again. Which one is simply the
image name it was installed from (`OS_CHANNEL` below), and an admin can move
it later (Admin → SiloOS → Updates → Channel; siloosd only ever switches
between tags of the repository the image names in `/usr/lib/siloos/release`).

| Channel | Tag | What it gets | Made by |
| --- | --- | --- | --- |
| **local** | `localhost/silo-os:dev` | Nothing: a build of your working tree, which follows no channel. Update it by installing again, or move it to a channel | `make os-image` |
| **master** | `silo-os:master` | A build of every push to `master` (each also kept as `silo-os:master-<sha>`) | `images.yml` on a push, `make nightly` |
| **latest** | `silo-os:latest` | Stable releases: the highest full `vX.Y.Z` | `images.yml` on a `v*` tag, `make release` |
| **pinned** | `silo-os:v0.4.2` (or `:master-<sha>`) | That one build, for ever: nothing newer arrives until it is moved | the same |

The OS and Silo are one release: a `silo-os` image runs the `silo` and
`silo-caddy` of its own tag (`master-<sha>` or `vX.Y.Z`), never a moving one,
just as a control plane runs the Bot images of its own commit. A local build
runs the nightly's (`silo:master`). Moving back to an older version is refused
(the database may already be newer than it understands); roll back instead.

## Build

The image, from your working tree:

```sh
make os-image                    # localhost/silo-os:dev (runs silo:master and silo-caddy:master)
make caddy-image                 # localhost/silo-caddy:dev, if you are changing Caddy's DNS providers
```

The context is `images/os/` and `build/bin/siloosd` (`images/os/Containerfile.dockerignore`). The
Postgres image is pinned by digest; the base follows `fedora-bootc:43` (Fedora
rebuilds it with fixes and quay.io drops old digests within days, so a pin
stops resolving); the last step is
`bootc container lint`. `images.yml` builds and publishes `silo-os` for amd64
and arm64 beside the other images, `silo-os` last, after the `silo` it names.

Then the media, with [bootc-image-builder](https://github.com/osbuild/bootc-image-builder)
(followed by its `latest` tag, the only one it publishes). It needs a rootful, privileged Podman, so these run under
`sudo` and ask for your password. Each takes `OS_CHANNEL` (`local`, the
default, installs your `make os-image`; `latest`, `master` or `v0.4.2` pull
that tag from ghcr.io) and writes under `build/os/`.

**The media are the same for everyone.** No user, key or setting is baked
in: one ISO or one qcow2 installs any number of machines. A machine learns
who you are at first boot, from cloud-init when something hands it user-data
(a cloud, Proxmox, Incus, libvirt), and otherwise it needs no login at all:
its console shows where Silo is and the first admin (§ First boot), and
everything after that is the web console.

```sh
make os-iso                                       # build/os/iso/bootiso/install.iso, for bare metal
make os-disk                                      # build/os/qcow2/qcow2/disk.qcow2 (libvirt, Proxmox, QEMU, Incus, OpenStack, Hetzner…)
make os-disk OS_DISK_TYPE=raw OS_CHANNEL=master   # a raw disk to dd onto a drive
make os-disk OS_DISK_TYPE=ami OS_CHANNEL=latest   # AWS; also vmdk (VMware), vhd (Azure), gce (Google Cloud)
```

- **The ISO is unattended:** it installs to the first disk it finds and
  **erases it**. On a machine with several disks, name the one in a kickstart
  (`images/os/bib.toml`, from `images/os/bib.toml.example`).
- **A disk image's root** grows to fill a larger virtual disk at first boot
  (`bootc-generic-growpart`), so give the VM the disk size you want. For a
  raw disk written to a drive, set the size in `images/os/bib.toml`.
- `images/os/bib.toml` is only ever bootc-image-builder options (sizes, the
  installer's disk), never a person.

### cloud-init

The image carries cloud-init. Handed user-data, it sets up the login and the
host name and can write Silo's first settings; with nothing to read it turns
itself off. `images/os/cloud-init/user-data.example.yaml` is the place to start:

- `users: [default]` is a user `admin` with the key the cloud provider holds;
  `ssh_authorized_keys` adds yours.
- `write_files` can write Silo's first `silo.yaml`: the first admin, Public
  URL, certificates. It is the file Admin → Settings edits afterwards, so
  nothing is locked in the console. With a first admin named there, none is
  generated and none is shown on the console.

Paste it where your platform takes user-data (a cloud's "user data" box,
Proxmox's cloud-init tab, `incus config set … cloud-init.user-data`), or make
a NoCloud seed for a plain QEMU or libvirt VM:

```sh
cp images/os/cloud-init/user-data.example.yaml images/os/cloud-init/user-data.yaml   # your key, your admin
make os-seed                                                           # build/os/seed/seed.iso, labelled cidata
```

A new `instance-id` in `images/os/cloud-init/meta-data.yaml` makes cloud-init run its
first-boot steps again on the same disk.

### Incus

```sh
make os-image os-disk os-incus     # build, make the qcow2 (sudo), import it as the image silo-os

incus init silo-os silo --vm -c limits.cpu=4 -c limits.memory=8GiB -d root,size=60GiB
incus start silo
incus console silo                 # the banner: https://<address> and the first admin (Ctrl+a q leaves)
```

Then open `https://<address>` (also in `incus list silo`); the browser warns
once about Silo's own certificate. With a login of your own (SSH, or your own
first admin), hand it user-data before the first start:

```sh
incus init silo-os silo --vm -c limits.cpu=4 -c limits.memory=8GiB -d root,size=60GiB
incus config set silo cloud-init.user-data - < images/os/cloud-init/user-data.yaml
incus config device add silo cloud-init disk source=cloud-init:config
incus start silo
ssh admin@$(incus list silo -c 4 -f csv | cut -d' ' -f1)
```

- `cloud-init:config` is Incus's NoCloud drive, which SiloOS's cloud-init
  reads (it has no Incus agent).
- Bots run as containers inside the VM, on its rootless Podman: nested in a
  VM they need no setting on Incus's side.
- A new build is `make os-image os-disk os-incus` again (`--reuse` replaces
  the image) and a new VM; a VM made from a channel (`OS_CHANNEL=latest`)
  updates itself instead.
- If the VM does not start under Secure Boot, add
  `-c security.secureboot=false` to `incus init`.
- `incus console silo --type=vga` is the screen instead of the serial port.

### A machine already running bootc

Fedora, CentOS Stream or RHEL bootc machines switch over in place, onto the
channel they name:

```sh
sudo bootc switch ghcr.io/d-513/silo-os:latest && sudo reboot
```

## First boot

1. The console (screen and serial port) says where Silo is and, unless
   user-data named your own, the first admin's email and password:

   ```
   SiloOS  ·  https://192.168.1.20
   First sign-in: admin@silo.local / k7mp-x3qa-9ten  (change the password once in)
   ```

   The banner is written at every boot. Once the first admin has signed in
   (Silo leaves `/var/lib/silo/data/first-admin-signed-in`), the next boot
   shows no password, only where to find it (`/etc/silo/secrets.env`).

   The first start pulls the images, which takes a few minutes. With a shell
   (`ssh admin@…`), `journalctl --user -M silo@ -u silo -f` shows it, and
   `sudo cat /etc/silo/secrets.env` has the same first admin.
2. Open `https://<address>` (the browser warns once: the certificate is Silo's
   own until there is a domain; `http://<address>:8080` also answers) and
   sign in. The first admin's first sign-in opens Admin. Change the password on the Account page: the console's line is
   only the first one, and goes stale once you do.
3. For a domain, point its DNS here and set Admin → Settings → Server →
   Public URL to `https://<that name>`. Within 15 s Caddy has a Let's Encrypt
   certificate for it (Admin → Settings → HTTPS, below).

Everything else (providers, models, mail) is Admin → Settings; the machine
itself is Admin → SiloOS. A machine installed from the ISO has no login but
the web console: it needs none.

## HTTPS

Caddy (`silo-caddy`) is configured by the control plane, never by hand: it
sends Caddy its whole configuration through the admin socket and sends it
again whenever Caddy's differs (every 15 s). What it serves:

- **The machine's own host name and IPv4 addresses**, always, with Silo's own
  certificate authority (Caddy's `internal` issuer): HTTPS on the local
  network from the first boot, by name or by bare address.
- **Public URL's name** (https, port 443), with the issuer `caddy.mode` picks:
  `auto` (the default: Let's Encrypt for a public domain, Silo's own
  authority for an address or a local name such as `silo.lan`), `acme`
  (always Let's Encrypt), `internal` (always Silo's own) or `off` (Caddy
  serves nothing at all).
- **The content host** (`content.host`, tunnels, live apps, public links)
  under the same issuer: with Let's Encrypt over port 80, one certificate per
  name at its first visit (Caddy asks the control plane first, so only names
  that exist); through a DNS provider or Silo's own authority, one wildcard.

A **DNS provider** (`caddy.dns_provider` and its settings, Admin → Settings →
HTTPS) answers Let's Encrypt through DNS instead of port 80: nothing has to
reach the machine from outside, and the content host gets a wildcard.
`silo-caddy` has Cloudflare, Route 53, Azure, Bunny, deSEC, DigitalOcean,
Duck DNS, Gandi, GoDaddy, Hetzner, IONOS, Linode, Namecheap, netcup, OVHcloud,
Porkbun and Vultr compiled in (Caddy loads no plugins at run time; a new
provider is a line in `images/caddy/Containerfile` and one in
`internal/caddy/dns.go`). Give a provider the narrowest credential it takes
(for Cloudflare, a token with Zone → DNS → Edit on the one zone). The
credential is a secret setting in `silo.yaml`, and Caddy keeps its last
configuration, the credential included, in its own volume (readable by
`silo` only).

**Silo's own authority**: its root certificate is on Admin → SiloOS and
Admin → Settings → HTTPS (**Root certificate**). Installed on a device, Silo's
own certificates are trusted there and the browser stops warning.

## siloosd

siloosd is root on the host, and the control plane, which faces the internet
and runs tools models choose, can ask it things. So what it offers is narrow
on purpose, and must stay that way:

- **Fixed operations, validated arguments, no shell.** Read the machine
  (hostname, OS image, uptime, memory, disks, clock); set the host name, the
  time zone, NTP; read and change a *wired* NetworkManager connection's IPv4
  settings; bootc check / download / roll back and the daily update policy;
  move to another channel; reboot and power off; read the journal of a fixed
  list of sources. There is no generic command, file write or package
  install, and no switch to an image the caller names (that would boot any
  image: root code). A channel is only ever a tag (`latest`, `master`,
  `vX.Y.Z`) of the repository the running image itself names
  (`/usr/lib/siloos/release`).
- **Who may call.** The socket's folder is `root:silo 0750` and only the
  control plane's container binds it; siloosd also checks the caller's uid
  (`SO_PEERCRED`: root or `silo`). In the control plane only instance admins'
  RPCs reach it (`access.RPCs`), never a Bot tool. Every change is a line in
  siloosd's journal with the caller's uid (Admin → SiloOS → Logs → Host
  daemon).
- **Network changes undo themselves.** A change runs under a NetworkManager
  checkpoint (`CheckpointCreate`, 120 s by default). The page then asks to keep
  it; unless it is kept from the console in time, NetworkManager puts the old
  settings back, so a change that cuts the admin off cannot strand the
  machine. A checkpoint does not survive a reboot: restarting inside the
  window keeps the new settings.
- **Updates are the admin's choice.** The base image's
  `bootc-fetch-apply-updates.timer`, which reboots whenever it finds a new
  image, is masked. siloosd's policy (Off, Download, Download and restart, at
  an hour in the machine's zone; default Download at 04:00) replaces it.

## Operate

```sh
sudo systemctl --user -M silo@ status silo silodb silocaddy
sudo journalctl --user -M silo@ -u silo -f
sudo -u silo XDG_RUNTIME_DIR=/run/user/1000 podman ps -a
sudo bootc status          # the image this machine runs, and the next one
sudo bootc upgrade --apply # update now (and reboot)
sudo bootc rollback        # back to the previous image
```

Updates follow the machine's channel (§ Channels). Admin → SiloOS → Updates
moves it between `latest`, `master` and the version it runs; `bootc switch`
from a shell can point it anywhere, which on purpose the console cannot.

## Not done yet

- **Signed images.** Nothing signs `silo-os` yet, so a machine trusts the
  registry: sign in CI (cosign) and have bootc require it
  (`/etc/containers/policy.json`).
- **Built media in CI.** `images.yml` publishes the image; the ISO and the
  cloud images are made with `make` (sudo) and not attached to a release.
- **Wi-Fi, IPv6, bonds and VLANs** are shown, not edited: siloosd changes a
  wired connection's IPv4 only.
- **SSH keys and a firewall** are not managed from the console yet.
- **Port 8080 is plain HTTP on every interface**, because the Bots' boxes
  reach the control plane there. It should answer only them.
- **Not tried on a real machine yet:** whether rootless Podman's forwarding
  makes a Bot box's connection to the control plane look like it comes from
  `127.0.0.1`. If it does, `auth.trusted_proxies=127.0.0.1` would let a Bot
  set its own `X-Forwarded-For`, and the Caddy ask endpoint would answer it.
- **Files a Bot writes** belong to a subordinate uid of `silo` on the host
  (rootless Podman maps the box's uid 1000 there), as in rootless development:
  the control plane cannot delete them by itself.
- **Backups** of `silodb-data` and `/var/lib/silo`.
- **Images inside the OS.** The six images are pulled on the machine at first
  start; carrying them in the image (an additional image store) would make an
  install work offline.
