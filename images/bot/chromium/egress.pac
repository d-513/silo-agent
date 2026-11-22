// Chromium's proxy choice behind the network filter (start.sh fills in the
// gateway's address and hands this to Chromium as managed policy).
//
// The gateway answers every name with itself and listens transparently on 80
// and 443 only, so a page that reaches another port (a speed test's servers on
// 8080, a dev server on 3000) would hit a closed port. Those go to the
// gateway's HTTP proxy door instead, as a CONNECT naming the real host and
// port, where they are decrypted and judged like any web request. A port
// browsers refuse anyway (mail, ssh) is refused there too unless the owner
// turned Other protocols on.
var GATEWAY = "PROXY __GATEWAY__:8888";

function FindProxyForURL(url, host) {
  var m = /^([a-z][a-z0-9+.-]*):\/\/(\[[^\]]*\]|[^\/?#:]*)(?::(\d+))?/i.exec(url);
  if (!m) {
    return "DIRECT";
  }
  var scheme = m[1].toLowerCase();
  var port = m[3] ? parseInt(m[3], 10) : (scheme == "https" || scheme == "wss" ? 443 : 80);
  if (port == 80 || port == 443) {
    return "DIRECT";
  }
  // The box's own services (a dev server, Chromium's debugging port).
  if (isPlainHostName(host) || host == "localhost" || shExpMatch(host, "127.*") || host == "[::1]" || host == "::1") {
    return "DIRECT";
  }
  return GATEWAY;
}
