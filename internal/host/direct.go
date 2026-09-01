package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

type directSession struct {
	engine     *engine
	connection *websocket.Conn
	cancel     context.CancelFunc
}

func serveDirect(ctx context.Context, cfg Config, probe *engine, log func(string, ...any)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if probe == nil {
		var err error
		probe, err = newEngine(cfg, log)
		if err != nil {
			return err
		}
		probe.terminate()
	}
	variants := probe.variants
	if len(variants) == 0 {
		variants = []string{"chess"}
	}
	info := struct {
		Version    int      `json:"version"`
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Variants   []string `json:"variants"`
		MinThreads int      `json:"minThreads"`
		MaxThreads int      `json:"maxThreads"`
		MaxHash    int      `json:"maxHash"`
	}{1, "__direct_native", probe.name, variants, probe.minThreads, probe.maxThreads, probe.maxHash}

	var ownership sync.Mutex
	var current *directSession
	var sessions sync.WaitGroup
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})
	mux.HandleFunc("GET /v1/uci", func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		sessions.Add(1)
		defer sessions.Done()
		defer connection.Close(websocket.StatusNormalClosure, "session ended")
		sessionCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		engine, err := newEngine(cfg, log)
		if err != nil {
			log("Direct engine: %v", err)
			return
		}
		session := &directSession{engine: engine, connection: connection, cancel: cancel}
		defer engine.terminate()
		go func() {
			defer cancel()
			for {
				select {
				case <-sessionCtx.Done():
					return
				case line, ok := <-engine.output:
					if !ok || line.err != nil {
						return
					}
					if err := connection.Write(sessionCtx, websocket.MessageText, []byte(strings.TrimSpace(line.command+" "+line.params))); err != nil {
						return
					}
				}
			}
		}()
		for {
			_, message, err := connection.Read(sessionCtx)
			if err != nil {
				return
			}
			command := strings.TrimSpace(string(message))
			if command == "go" || strings.HasPrefix(command, "go ") {
				ownership.Lock()
				if sessionCtx.Err() != nil {
					ownership.Unlock()
					return
				}
				if current != nil && current != session {
					current.engine.send("stop")
					current.connection.Close(websocket.StatusNormalClosure, "superseded")
					current.cancel()
					current.engine.terminate()
				}
				current = session
				ownership.Unlock()
			}
			if err := engine.send(command); err != nil {
				return
			}
		}
	})
	allowedOrigin := strings.TrimRight(cfg.Host, "/")
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != allowedOrigin {
			log("Rejecting %s %s: origin %q, allowed %q", r.Method, r.URL.Path, origin, allowedOrigin)
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		mux.ServeHTTP(w, r)
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:48036")
	if err != nil {
		return fmt.Errorf("direct listener: %w", err)
	}
	defer listener.Close()
	defer server.Close()
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	log("Listening at http://127.0.0.1:48036")
	select {
	case err = <-failed:
	case <-ctx.Done():
		err = ctx.Err()
	}
	cancel()
	server.Close()
	sessions.Wait()
	return err
}
