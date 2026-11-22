package siloosd

import (
	"bufio"
	"context"
	"runtime"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// disks are the filesystems the System page shows: the OS image's and the
// one holding /var (the database, the Bots' workspaces, images). On most
// installs they are one, shown once.
var disks = []struct{ label, path string }{{"system", "/sysroot"}, {"data", "/var"}}

func (s *Server) GetSystem(ctx context.Context, _ *connect.Request[v1.GetSystemRequest]) (*connect.Response[v1.SystemInfo], error) {
	return connect.NewResponse(s.system(ctx)), nil
}

func (s *Server) system(ctx context.Context) *v1.SystemInfo {
	out := &v1.SystemInfo{
		ApiVersion: APIVersion,
		Hostname:   s.hostname(ctx),
		OsName:     osName(s.readFile("usr/lib/os-release")),
		Kernel:     strings.TrimSpace(s.readFile("proc/sys/kernel/osrelease")),
		Cpus:       int32(runtime.NumCPU()),
		Now:        s.now().UTC().Format(time.RFC3339),
	}
	if f := strings.Fields(s.readFile("proc/uptime")); len(f) > 0 {
		if up, err := strconv.ParseFloat(f[0], 64); err == nil {
			out.UptimeSeconds = int64(up)
		}
	}
	if f := strings.Fields(s.readFile("proc/loadavg")); len(f) > 0 {
		out.Load1, _ = strconv.ParseFloat(f[0], 64)
	}
	out.MemoryTotal, out.MemoryAvailable = meminfo(s.readFile("proc/meminfo"))
	seen := map[string]bool{}
	for _, d := range disks {
		st, ok := diskOf(s.file(d.path))
		if !ok || seen[st.id] {
			continue
		}
		seen[st.id] = true
		out.Disks = append(out.Disks, &v1.SystemDisk{Label: d.label, Path: d.path, Total: st.total, Free: st.free})
	}
	if raw, err := s.run.Run(ctx, "timedatectl", "show"); err == nil {
		kv := keyValues(string(raw))
		out.TimeZone, out.Ntp, out.NtpSynced = kv["Timezone"], kv["NTP"] == "yes", kv["NTPSynchronized"] == "yes"
	}
	if st, err := s.bootcStatus(ctx); err == nil && st.Status.Booted != nil && st.Status.Booted.Image != nil {
		out.Image = st.Status.Booted.Image.Image.Image
		out.ImageVersion = st.Status.Booted.Image.Version
	}
	return out
}

func (s *Server) hostname(ctx context.Context) string {
	if raw, err := s.run.Run(ctx, "hostnamectl", "hostname"); err == nil {
		return strings.TrimSpace(string(raw))
	}
	return ""
}

func (s *Server) SetHostname(ctx context.Context, req *connect.Request[v1.SetHostnameRequest]) (*connect.Response[v1.SystemInfo], error) {
	h := strings.ToLower(strings.TrimSpace(req.Msg.GetHostname()))
	if !validHostname(h) {
		return nil, invalid("A host name is letters, digits and hyphens, in dot-separated parts of at most 63 characters, and does not start or end with a hyphen.")
	}
	if _, err := s.run.Run(ctx, "hostnamectl", "set-hostname", h); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	audit(ctx, "set the host name to %s", h)
	return connect.NewResponse(s.system(ctx)), nil
}

func (s *Server) SetTime(ctx context.Context, req *connect.Request[v1.SetTimeRequest]) (*connect.Response[v1.SystemInfo], error) {
	if z := strings.TrimSpace(req.Msg.GetTimeZone()); z != "" {
		if !validZone(z) {
			return nil, invalid("That is not a time zone name (Europe/Warsaw, UTC).")
		}
		raw, err := s.run.Run(ctx, "timedatectl", "list-timezones")
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if !hasLine(string(raw), z) {
			return nil, invalid("This machine does not know the time zone " + z + ".")
		}
		if _, err := s.run.Run(ctx, "timedatectl", "set-timezone", z); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		audit(ctx, "set the time zone to %s", z)
	}
	if req.Msg.Ntp != nil {
		on := "false"
		if req.Msg.GetNtp() {
			on = "true"
		}
		if _, err := s.run.Run(ctx, "timedatectl", "set-ntp", on); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		audit(ctx, "set NTP %s", on)
	}
	return connect.NewResponse(s.system(ctx)), nil
}

// osName is PRETTY_NAME (else NAME) of an os-release file.
func osName(raw string) string {
	kv := keyValues(raw)
	if n := kv["PRETTY_NAME"]; n != "" {
		return n
	}
	return kv["NAME"]
}

// keyValues reads KEY=value lines (os-release, timedatectl show), unquoting
// a value in double or single quotes.
func keyValues(raw string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || k == "" || strings.HasPrefix(k, "#") {
			continue
		}
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}

// meminfo is MemTotal and MemAvailable, in bytes.
func meminfo(raw string) (total, avail int64) {
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			avail = n * 1024
		}
	}
	return total, avail
}

func hasLine(raw, want string) bool {
	for _, l := range strings.Split(raw, "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}
