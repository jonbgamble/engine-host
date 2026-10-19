package host

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
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

func serveDirect(ctx context.Context, cfg Config, log func(string, ...any), storeEngineName func(string, string) error, ready func()) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	probe, err := newEngine(cfg, log)
	if err != nil {
		return err
	}
	probe.terminate()
	if err := storeEngineName(cfg.Engine, probe.name); err != nil {
		return err
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
	}{1, "__direct_native", probe.name, variants, 1, cfg.MaxThreads, cfg.MaxHash}
	certificate, _, err := directCertificate()
	if err != nil {
		return fmt.Errorf("direct certificate: %w", err)
	}
	fingerprint := fmt.Sprintf("%X", sha256.Sum256(certificate.Certificate[0]))
	setup := template.Must(template.New("setup").Parse(directSetupPage))

	var ownership sync.Mutex
	var current *directSession
	var sessions sync.WaitGroup
	mux := http.NewServeMux()
	mux.HandleFunc("GET /setup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		setup.Execute(w, struct {
			Secure bool
			Host, Fingerprint string
		}{r.TLS != nil, strings.TrimRight(cfg.Host, "/"), fingerprint})
	})
	mux.HandleFunc("GET /certificate.cer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pkix-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="lichess-engine-host.cer"`)
		w.Write(certificate.Certificate[0])
	})
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
	server := &http.Server{TLSConfig: &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion: tls.VersionTLS12,
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != strings.TrimRight(cfg.Host, "/") {
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
	secureListener, err := net.Listen("tcp", "127.0.0.1:48037")
	if err != nil {
		return fmt.Errorf("secure direct listener: %w", err)
	}
	defer secureListener.Close()
	defer server.Close()
	failed := make(chan error, 2)
	go func() { failed <- server.Serve(listener) }()
	go func() { failed <- server.ServeTLS(secureListener, "", "") }()
	log("Direct mode ready at http://127.0.0.1:48036 and https://127.0.0.1:48037")
	log("Safari certificate SHA-256: %s", fingerprint)
	ready()
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

const directSetupPage = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Lichess Engine Host</title>
<style>
body{max-width:40rem;margin:3rem auto;padding:0 1.5rem;background:#f6f8fa;color:#24292e;font:17px/1.6 "Trebuchet MS",sans-serif}
h1{font-size:1.8rem}a{color:#0969a2}li{margin-bottom:1rem}code{overflow-wrap:anywhere;font-size:.85rem}
.action{display:inline-block;padding:.6rem 1rem;background:#0969a2;color:white;text-decoration:none;border-radius:4px}
</style>
<h1>Lichess Engine Host</h1>
{{if .Secure}}
<h2>Secure connection accepted</h2>
<p>Return to analysis and connect your local engine. If Safari still cannot connect, explicitly trust the certificate for SSL in Keychain Access.</p>
<p><a class="action" href="{{.Host}}/analysis">Return to analysis</a></p>
{{else}}
<h2>Safari certificate setup</h2>
<p>Your engine is running on this computer. Safari needs a trusted certificate for the secure local connection. Other browsers can normally connect without this setup.</p>
<ol>
<li><a href="/certificate.cer" download>Download this installation's certificate</a>, then import it into the <strong>login</strong> keychain in <strong>Keychain Access</strong>.</li>
<li>Open the <strong>Lichess Engine Host</strong> certificate and expand <strong>Trust</strong>. Set <strong>Secure Sockets Layer (SSL)</strong> to <strong>Always Trust</strong>. Close the window and authenticate if macOS asks.</li>
<li><a href="https://127.0.0.1:48037/setup">Check the secure connection</a>, then return to analysis.</li>
</ol>
<p>This trusts only this installation's server certificate. It is not a general-purpose certificate authority. Trust persists independently of Safari website data; replacing or expiring the certificate requires setup again.</p>
{{end}}
<p>Certificate SHA-256, also shown in the engine-host activity log:</p>
<p><code>{{.Fingerprint}}</code></p>
</html>`