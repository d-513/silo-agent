#!/bin/bash
export DISPLAY=:1
export HOME=/home/silo
export USER=silo
export LOGNAME=silo
export XDG_RUNTIME_DIR=/run/user/1000
export SILO_WORKSPACE=/workspace
export SILO_WORKER_SOCK=/var/run/silo/worker.sock
export PYTHONPATH=/opt/silo

mkdir -p /var/run/silo /run/user/1000 /workspace /workspace/bot /opt/silo/tools
chown silo:silo /var/run/silo /run/user/1000 /opt/silo/tools /workspace /workspace/bot
chmod 1777 /workspace /workspace/bot
chmod 700 /run/user/1000

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
cp /opt/silo/tint2/tint2rc /home/silo/.config/tint2/tint2rc
cp /opt/silo/gtk-3.0/settings.ini /home/silo/.config/gtk-3.0/settings.ini
cp /opt/silo/gtk-3.0/bookmarks /home/silo/.config/gtk-3.0/bookmarks
cp /opt/silo/thunar/thunarrc /home/silo/.config/Thunar/thunarrc
cp /opt/silo/mimeapps.list /home/silo/.config/mimeapps.list
chown silo:silo /home/silo /home/silo/chrome-profile
chown -R silo:silo /home/silo/.config /opt/silo/ubol

run Xvfb :1 -screen 0 1280x720x24 -ac +extension RANDR >/tmp/xvfb.log 2>&1
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
