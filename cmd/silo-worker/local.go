package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/security"
)

func serveLocal(sock string, w *worker) {
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		log.Printf("unix: %v", err)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/secrets/get", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Name  string `json:"name"`
			RunID string `json:"run_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		val, err := w.getSecret(r.Context(), body.Name, body.RunID)
		rw.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.mask.Add(val)
		_ = json.NewEncoder(rw).Encode(map[string]string{"value": val})
	})
	mux.HandleFunc("/v1/tools/call", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Connector string         `json:"connector"`
			Action    string         `json:"action"`
			Args      map[string]any `json:"args"`
			RunID     string         `json:"run_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Connector == "desktop" {
			raw, _ := json.Marshal(body.Args)
			redacted := security.Redact("desktop", body.Action, string(raw))
			if _, err := w.callTool(r.Context(), body.Connector, body.Action, redacted, body.RunID); err != nil {
				rw.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
				return
			}
			val, err := w.runDesktop(r.Context(), body.Action, body.Args)
			rw.Header().Set("Content-Type", "application/json")
			if err != nil {
				_ = json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
				return
			}
			var parsed any
			if json.Unmarshal([]byte(val), &parsed) != nil {
				parsed = val
			}
			_ = json.NewEncoder(rw).Encode(map[string]any{"result": parsed})
			return
		}
		args, _ := json.Marshal(body.Args)
		val, err := w.callTool(r.Context(), body.Connector, body.Action, string(args), body.RunID)
		rw.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
			return
		}
		var parsed any
		if json.Unmarshal([]byte(val), &parsed) != nil {
			parsed = val
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"result": parsed})
	})
	mux.HandleFunc("/v1/chrome/ensure", func(rw http.ResponseWriter, r *http.Request) {
		st, err := w.ensureChrome(r.Context())
		rw.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]string{"status": st})
	})
	log.Printf("local tools on %s", sock)
	_ = http.Serve(ln, mux)
}

func (w *worker) getSecret(ctx context.Context, name, runID string) (string, error) {
	res, err := w.rpc.GetSecret(ctx, connect.NewRequest(&v1.SecretReq{Name: name, RunId: runID}))
	if err != nil {
		return "", err
	}
	if res.Msg.GetError() != "" {
		return "", errors.New(res.Msg.GetError())
	}
	return res.Msg.GetValue(), nil
}

func (w *worker) callTool(ctx context.Context, connector, action, argsJSON, runID string) (string, error) {
	res, err := w.rpc.CallTool(ctx, connect.NewRequest(&v1.ToolReq{
		Connector: connector, Action: action, ArgsJson: argsJSON, RunId: runID,
	}))
	if err != nil {
		return "", err
	}
	if res.Msg.GetError() != "" {
		return "", errors.New(res.Msg.GetError())
	}
	out := res.Msg.GetResultJson()
	return w.mask.Apply(out), nil
}
