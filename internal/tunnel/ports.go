package tunnel

import "fmt"

// reserved are ports the box runs for itself. A tunnel to either would hand out
// the desktop or the browser's remote-control socket.
var reserved = map[int]string{5900: "x11vnc", 9222: "Chromium debugging"}

// CheckPort reports why port may not be tunnelled, or nil. The CP checks it
// when a tunnel is declared and the worker checks it again before dialing.
func CheckPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d is out of range (1-65535)", port)
	}
	if what, ok := reserved[port]; ok {
		return fmt.Errorf("port %d is reserved (%s)", port, what)
	}
	return nil
}
