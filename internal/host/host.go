package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Engine    string      `json:"engine"`
	SetOption [][2]string `json:"setoption,omitempty"`
	// Host      string      `json:"host"`
}

type App struct {
	mu        sync.Mutex
	path      string
	cfg       Config
	status    []string
	notify    func()
	cancel    context.CancelFunc
	done      <-chan struct{}
	closing   bool
	logQueue  chan string
	logStop   chan struct{}
	logDone   chan struct{}
	logClosed atomic.Bool
}

func New(notify func()) (*App, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	if err := saveConfig(path, cfg); err != nil {
		return nil, err
	}
	app := &App{path: path, cfg: cfg, notify: notify}
	app.startLogging()
	app.Log("Configuration: %s", path)
	return app, nil
}

func (app *App) Config() Config {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.cfg
}

func (app *App) Status() string {
	app.mu.Lock()
	defer app.mu.Unlock()
	return strings.Join(app.status, "\n")
}

func (app *App) Log(format string, args ...any) {
	if app.logClosed.Load() {
		return
	}
	line := time.Now().Format("15:04:05.000 ") + fmt.Sprintf(format, args...)
	select {
	case app.logQueue <- line:
	default:
	}
}

func (app *App) startLogging() {
	app.logQueue = make(chan string, 256)
	app.logStop = make(chan struct{})
	app.logDone = make(chan struct{})
	go func() {
		defer close(app.logDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				app.flushLogs(true)
			case <-app.logStop:
				app.flushLogs(false)
				return
			}
		}
	}()
}

func (app *App) flushLogs(notify bool) {
	var batch []string
	for range len(app.logQueue) {
		batch = append(batch, <-app.logQueue)
	}
	if len(batch) == 0 {
		return
	}
	app.mu.Lock()
	app.status = append(app.status, batch...)
	app.status = app.status[max(0, len(app.status)-200):]
	app.mu.Unlock()
	if notify && app.notify != nil {
		app.notify()
	}
}

func (app *App) SetEngine(path string) {
	cfg := app.Config()
	cfg.Engine = path
	probe, err := newEngine(cfg, app.Log)
	if err != nil {
		app.Log("Selecting engine: %v", err)
		return
	}
	probe.terminate()
	app.mu.Lock()
	if err := saveConfig(app.path, cfg); err != nil {
		app.mu.Unlock()
		app.Log("Saving configuration: %v", err)
		return
	}
	app.cfg = cfg
	app.mu.Unlock()
	app.Log("Engine: %s", path)
	app.start(probe)
}

func (app *App) Close() {
	app.mu.Lock()
	app.closing = true
	if app.cancel != nil {
		app.cancel()
	}
	done := app.done
	app.mu.Unlock()
	if done != nil {
		<-done
	}
	if app.logDone != nil {
		if !app.logClosed.Swap(true) {
			close(app.logStop)
		}
		<-app.logDone
	}
}

func (app *App) Start() {
	app.start(nil)
}

func (app *App) start(probe *engine) {
	app.mu.Lock()
	if app.closing {
		app.mu.Unlock()
		return
	}
	if app.cancel != nil {
		app.cancel()
	}
	cfg := app.cfg
	if cfg.Engine == "" {
		app.cancel = nil
		app.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	previous := app.done
	done := make(chan struct{})
	app.cancel = cancel
	app.done = done
	app.mu.Unlock()
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		if ctx.Err() != nil {
			return
		}
		if err := serveDirect(ctx, cfg, probe, app.Log); err != nil && !errors.Is(err, context.Canceled) {
			app.Log("Host stopped: %v", err)
		}
	}()
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "direct-engine-host.json"), nil
}

func loadConfig(path string) (Config, error) {
	cfg := Config{} //Host: "https://lichess.org"}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(filepath.Join(filepath.Dir(path), "lichess-engine-host.json"))
	}
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(data, &cfg)
}

func saveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
