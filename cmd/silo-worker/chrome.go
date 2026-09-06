package main

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const cdpAddr = "127.0.0.1:9222"

func wantsPlaywright(code string) bool {
	s := strings.ToLower(code)
	return strings.Contains(s, "chrome_page") ||
		strings.Contains(s, "playwright") ||
		strings.Contains(s, "connect_over_cdp")
}

func chromeUp() bool {
	c, err := net.DialTimeout("tcp", cdpAddr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func (w *worker) ensureChrome(ctx context.Context) (string, error) {
	if chromeUp() {
		return "up", nil
	}
	cmd := exec.Command("silo-chromium")
	cmd.Env = w.childEnv("")
	if !envHas(cmd.Env, "DISPLAY=") {
		cmd.Env = append(cmd.Env, "DISPLAY=:1")
	}
	cmd.Dir = w.workspace
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if chromeUp() {
			return "started", nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "", errors.New("chromium did not open CDP on 127.0.0.1:9222")
}

func envHas(env []string, prefix string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}
