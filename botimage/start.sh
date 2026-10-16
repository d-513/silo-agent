#!/bin/bash
export DISPLAY=:1
export HOME=/home/silo
export USER=silo
export LOGNAME=silo
export XDG_RUNTIME_DIR=/run/user/1000
export SILO_WORKSPACE=/workspace
export SILO_WORKER_SOCK=/var/run/silo/worker.sock
export PYTHONPATH=/opt/silo

mkdir -p /var/run/silo /run/user/1000 /workspace /workspace/bot /opt/silo/tools /opt/silo/skills /var/lib/silo/history
# Chat uploads land in /workspace/tmp. Clear it every start so it cannot accumulate.
rm -rf /workspace/tmp
mkdir -p /workspace/tmp
# /var/lib/silo/history is the workspace history (change tracking): the worker's
# shadow git store, bound from beside the workspace. Only the mount point needs
# the chown; the worker makes everything inside it.
chown silo:silo /var/run/silo /run/user/1000 /opt/silo/tools /opt/silo/skills /workspace /workspace/bot /workspace/tmp /var/lib/silo/history
chmod 700 /var/lib/silo/history
chmod 1777 /workspace /workspace/bot /workspace/tmp
chmod 700 /run/user/1000

# Behind the network filter the box has no resolver of its own: point it at the
# egress gateway, which answers every name with itself so connections are
# intercepted. Podman ignores a container's --dns on a user-defined network, so
# it is set here rather than in the create.
if [ -n "$SILO_EGRESS_DNS" ]; then
  printf 'nameserver %s\noptions ndots:1 timeout:2 attempts:2\n' "$SILO_EGRESS_DNS" > /etc/resolv.conf
fi

# Ports other than 80 and 443 have no transparent door: the gateway answers
# every name with itself and listens only for web traffic there. They go
# through its SOCKS5 door (port 1080, egress.DefaultSOCKSAddr), which is open
# only while the Bot's owner has Other protocols on; the gateway decides, so
# the files below are written either way and a switch flip needs no restart.
# proxychains4 sends names to the proxy (proxy_dns), never to the local
# resolver, whose answer would be the gateway itself. ssh (and git over ssh)
# goes through it by default; a user's own ~/.ssh/config wins.
if [ -n "$SILO_EGRESS_DNS" ]; then
  socks_port=1080
  cat > /etc/proxychains4.conf <<EOF
strict_chain
proxy_dns
remote_dns_subnet 224
tcp_read_time_out 15000
tcp_connect_time_out 8000
localnet 127.0.0.0/255.0.0.0
localnet $SILO_EGRESS_DNS/255.255.255.255
[ProxyList]
socks5 $SILO_EGRESS_DNS $socks_port
EOF
  chmod 644 /etc/proxychains4.conf
  mkdir -p /etc/ssh/ssh_config.d
  cat > /etc/ssh/ssh_config.d/silo-egress.conf <<EOF
Match host *,!localhost,!127.0.0.1,!::1
    ProxyCommand nc -X 5 -x $SILO_EGRESS_DNS:$socks_port %h %p
EOF
  chmod 644 /etc/ssh/ssh_config.d/silo-egress.conf
  export SILO_SOCKS_PROXY="socks5h://$SILO_EGRESS_DNS:$socks_port"
fi

# The gateway decrypts HTTPS and signs what it hands back with its own CA, so
# the box trusts that CA: in the system bundle (OpenSSL, curl, git, Go,
# Python's ssl; the CP's env points requests, httpx, pip and Node at it too)
# and in the silo user's NSS store, which is what Chromium reads. The env is
# fixed when the box is created, so a rotated CA needs a Container reset.
if [ -n "$SILO_EGRESS_CA" ]; then
  ca=/usr/local/share/ca-certificates/silo-egress.crt
  printf '%s\n' "$SILO_EGRESS_CA" > "$ca"
  chmod 644 "$ca"
  update-ca-certificates >/dev/null 2>&1 || echo "silo: update-ca-certificates failed" >&2
  nss=/home/silo/.pki/nssdb
  runuser -u silo -- mkdir -p "$nss"
  if [ ! -f "$nss/cert9.db" ]; then
    runuser -u silo -- certutil -N -d "sql:$nss" --empty-password
  fi
  runuser -u silo -- certutil -D -d "sql:$nss" -n "Silo network filter" >/dev/null 2>&1
  runuser -u silo -- certutil -A -d "sql:$nss" -n "Silo network filter" -t "C,," -i "$ca" \
    || echo "silo: could not add the gateway certificate for Chromium" >&2
fi

children=()

kill_children() {
  trap - EXIT TERM INT
  for pid in "${children[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  for pid in "${children[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
}

die() {
  echo "silo: $*" >&2
  kill_children
  exit 1
}

trap 'die signal' TERM INT
trap 'kill_children' EXIT

# start.sh stays root for chown + the supervisor loop. Everything it launches is silo.
run() {
  runuser -u silo -- "$@" &
  children+=($!)
}

# Worker does not need X. Start it first so the bot is online while the desktop comes up.
run silo-worker

mkdir -p /home/silo/chrome-profile /home/silo/.config/openbox /home/silo/.config/tint2 \
  /home/silo/.config/gtk-3.0 /home/silo/.config/Thunar /usr/local/share/applications
cp /opt/silo/desktop/silo-*.desktop /usr/local/share/applications/ 2>/dev/null || true
cp /opt/silo/openbox/autostart /home/silo/.config/openbox/autostart
cp /opt/silo/openbox/rc.xml /home/silo/.config/openbox/rc.xml
cp /opt/silo/tint2/tint2rc /home/silo/.config/tint2/tint2rc
cp /opt/silo/gtk-3.0/settings.ini /home/silo/.config/gtk-3.0/settings.ini
cp /opt/silo/gtk-3.0/bookmarks /home/silo/.config/gtk-3.0/bookmarks
cp /opt/silo/thunar/thunarrc /home/silo/.config/Thunar/thunarrc
cp /opt/silo/mimeapps.list /home/silo/.config/mimeapps.list
chown silo:silo /home/silo /home/silo/chrome-profile
chown -R silo:silo /home/silo/.config /opt/silo/ubol

run Xvfb :1 -screen 0 1600x900x24 -ac +extension RANDR >/tmp/xvfb.log 2>&1
ok=0
for _ in $(seq 1 50); do
  if xset q >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 0.1
done
[ "$ok" = 1 ] || die "Xvfb did not become ready"

run openbox >/tmp/openbox.log 2>&1
runuser -u silo -- feh --bg-fill /opt/silo/wallpaper.png >/tmp/feh.log 2>&1 || xsetroot -solid "#2A3F5F"
run tint2 >/tmp/tint2.log 2>&1
# -speeds lan: skip the link-speed probe (VNC is localhost). Saves a few seconds.
run x11vnc -display :1 -localhost -forever -shared -rfbport 5900 -nopw -xkb -speeds lan >/tmp/x11vnc.log 2>&1
x11pid=${children[-1]}
ok=0
for _ in $(seq 1 50); do
  kill -0 "$x11pid" 2>/dev/null || die "x11vnc exited"
  if grep -q "Listening for VNC connections on TCP port 5900" /tmp/x11vnc.log 2>/dev/null; then
    ok=1
    break
  fi
  sleep 0.1
done
[ "$ok" = 1 ] || die "x11vnc did not bind 5900"

while true; do
  for pid in "${children[@]}"; do
    if ! kill -0 "$pid" 2>/dev/null; then
      die "pid $pid exited"
    fi
  done
  sleep 0.4
done
