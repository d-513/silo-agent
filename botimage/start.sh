#!/bin/bash
export DISPLAY=:1
export HOME=/home/bot
mkdir -p /home/bot/chrome-profile /home/bot/.config/openbox /home/bot/.config/tint2 \
  /home/bot/.config/gtk-3.0 /home/bot/.config/Thunar \
  /var/run/silo /workspace /usr/local/share/applications
cp /opt/silo/desktop/silo-*.desktop /usr/local/share/applications/ 2>/dev/null || true
cp /opt/silo/openbox/autostart /home/bot/.config/openbox/autostart
cp /opt/silo/tint2/tint2rc /home/bot/.config/tint2/tint2rc
cp /opt/silo/gtk-3.0/settings.ini /home/bot/.config/gtk-3.0/settings.ini
cp /opt/silo/thunar/thunarrc /home/bot/.config/Thunar/thunarrc

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

run() {
  "$@" &
  children+=($!)
}

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
sleep 0.2
feh --bg-fill /opt/silo/wallpaper.png >/tmp/feh.log 2>&1 || xsetroot -solid "#2A3F5F"
run tint2 >/tmp/tint2.log 2>&1
run x11vnc -display :1 -localhost -forever -shared -rfbport 5900 -nopw -xkb >/tmp/x11vnc.log 2>&1
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

run silo-worker

while true; do
  for pid in "${children[@]}"; do
    if ! kill -0 "$pid" 2>/dev/null; then
      die "pid $pid exited"
    fi
  done
  sleep 0.4
done
