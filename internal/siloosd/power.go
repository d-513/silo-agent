package siloosd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// powerDelay lets the answer reach the console before the machine goes.
const powerDelay = 3 * time.Second

func (s *Server) Power(ctx context.Context, req *connect.Request[v1.PowerRequest]) (*connect.Response[v1.PowerResponse], error) {
	action := req.Msg.GetAction()
	if action != "reboot" && action != "poweroff" {
		return nil, invalid("The action is reboot or poweroff.")
	}
	audit(ctx, "asked for %s", action)
	s.after(powerDelay, func() { s.reboot(action) })
	return connect.NewResponse(&v1.PowerResponse{At: s.now().Add(powerDelay).UTC().Format(time.RFC3339)}), nil
}

func (s *Server) reboot(action string) {
	if _, err := s.run.Run(context.Background(), "systemctl", action); err != nil {
		fmt.Fprintf(os.Stderr, "siloosd: %s: %v\n", action, err)
	}
}

const (
	logsDefault = 200
	logsMax     = 2000
	messageMax  = 4000
)

// logSources are the journal matches each source reads. Silo's containers
// run as silo's user units; their output is in the system journal under the
// user unit and the container's name.
var logSources = map[string][]string{
	"silo":      {"_SYSTEMD_USER_UNIT=silo.service", "+", "CONTAINER_NAME=silo"},
	"silodb":    {"_SYSTEMD_USER_UNIT=silodb.service", "+", "CONTAINER_NAME=silodb"},
	"silocaddy": {"_SYSTEMD_USER_UNIT=silocaddy.service", "+", "CONTAINER_NAME=silocaddy"},
	"siloosd":   {"_SYSTEMD_UNIT=siloosd.service"},
	// Matches on one field are either-or: siloosd runs the jobs, ostree
	// finishes a staged image on the way down.
	"updates": {"_SYSTEMD_UNIT=siloosd.service", "_SYSTEMD_UNIT=ostree-finalize-staged.service"},
	"system":  {"-p", "warning"},
}

func (s *Server) ReadLogs(ctx context.Context, req *connect.Request[v1.ReadLogsRequest]) (*connect.Response[v1.ReadLogsResponse], error) {
	match, ok := logSources[req.Msg.GetSource()]
	if !ok {
		return nil, invalid("The source is silo, silodb, silocaddy, siloosd, updates or system.")
	}
	n := int(req.Msg.GetLines())
	if n <= 0 {
		n = logsDefault
	}
	n = min(n, logsMax)
	args := append([]string{"--no-pager", "-o", "json", "-n", strconv.Itoa(n)}, match...)
	raw, err := s.run.Run(ctx, "journalctl", args...)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.ReadLogsResponse{Lines: journalLines(raw)}), nil
}

// journalLines reads journalctl's JSON, one entry a line, oldest first.
func journalLines(raw []byte) []*v1.LogLine {
	var out []*v1.LogLine
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		l := &v1.LogLine{Message: clip(field(e["MESSAGE"]), messageMax)}
		if us, err := strconv.ParseInt(field(e["__REALTIME_TIMESTAMP"]), 10, 64); err == nil {
			l.Time = time.UnixMicro(us).UTC().Format(time.RFC3339Nano)
		}
		if p, err := strconv.Atoi(field(e["PRIORITY"])); err == nil {
			l.Priority = int32(p)
		} else {
			l.Priority = 6
		}
		for _, k := range []string{"CONTAINER_NAME", "_SYSTEMD_USER_UNIT", "_SYSTEMD_UNIT", "SYSLOG_IDENTIFIER"} {
			if v := field(e[k]); v != "" {
				l.Unit = v
				break
			}
		}
		out = append(out, l)
	}
	return out
}

// field is a journal field's text: a string, or an array of bytes when the
// value was not valid UTF-8 (journalctl's way to say so).
func field(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b []byte
	var ints []int
	if json.Unmarshal(raw, &ints) == nil {
		for _, i := range ints {
			b = append(b, byte(i))
		}
		if utf8.Valid(b) {
			return string(b)
		}
		return strings.ToValidUTF8(string(b), "�")
	}
	return ""
}
