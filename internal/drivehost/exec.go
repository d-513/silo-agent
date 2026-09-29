package drivehost

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Exec is the real Runner: rclone processes and /proc/self/mountinfo.
type Exec struct {
	Rclone string // default "rclone"
}

func (e Exec) bin() string {
	if e.Rclone != "" {
		return e.Rclone
	}
	return "rclone"
}

type proc struct {
	cmd  *exec.Cmd
	tail *ring
	once sync.Once
	done chan struct{}
	err  error
}

func (e Exec) Start(args, env []string) (Proc, error) {
	cmd := exec.Command(e.bin(), args...)
	cmd.Env = env
	r := &ring{max: 8 << 10}
	cmd.Stderr = r
	cmd.Stdout = os.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &proc{cmd: cmd, tail: r, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *proc) Wait() error {
	<-p.done
	return p.err
}

// Stop sends SIGTERM (rclone unmounts cleanly on it), then SIGKILL.
func (p *proc) Stop() {
	p.once.Do(func() {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			_ = p.cmd.Process.Kill()
		}
	})
}

func (p *proc) Tail() string { return p.tail.String() }

func (e Exec) Output(ctx context.Context, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.bin(), args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, errors.New(msg)
	}
	return out, nil
}

// Mounted is true when path is a mount point in this mount namespace.
func (Exec) Mounted(path string) bool {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) > 4 && unescapeMount(fields[4]) == path {
			return true
		}
	}
	return false
}

// unescapeMount undoes mountinfo's octal escapes (\040 for a space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			n := 0
			ok := true
			for _, c := range s[i+1 : i+4] {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				n = n*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Unmount lazily detaches path (a dead FUSE daemon cannot answer a normal
// unmount).
func (Exec) Unmount(path string) error {
	if err := exec.Command("fusermount3", "-uz", path).Run(); err == nil {
		return nil
	}
	return exec.Command("umount", "-l", path).Run()
}

// ring keeps the last max bytes written and mirrors them to stderr.
type ring struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (r *ring) Write(p []byte) (int, error) {
	_, _ = os.Stderr.Write(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
	return len(p), nil
}

func (r *ring) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
