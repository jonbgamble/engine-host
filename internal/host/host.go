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
	"time"
)

type Config struct {
	Engine    string      `json:"engine"`
	SetOption [][2]string `json:"setoption,omitempty"`
	Host      string      `json:"host"`
}

type App struct {
	mu         sync.Mutex
	path       string
	cfg        Config
	status     []string
	notify     func()
	cancel     context.CancelFunc
	done       <-chan struct{}
	closing    bool
	connection string
	ready      bool
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

func (app *App) Connection() (bool, string) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.cfg.Engine == "" {
		return false, "No engine selected"
	}
	return app.ready, app.connection
}

func (app *App) Log(format string, args ...any) {
	line := time.Now().Format("15:04:05 ") + fmt.Sprintf(format, args...)
	app.mu.Lock()
	app.status = append(app.status, line)
	if len(app.status) > 200 {
		app.status = app.status[len(app.status)-200:]
	}
	notify := app.notify
	app.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func (app *App) SetEngine(path string) {
	app.mu.Lock()
	app.cfg.Engine = path
	app.connection, app.ready = "Connecting...", false
	cfg, configPath := app.cfg, app.path
	app.mu.Unlock()
	app.Log("Engine: %s", path)
	if err := saveConfig(configPath, cfg); err != nil {
		app.Log("Saving configuration: %v", err)
		return
	}
	app.Start()
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
}

func (app *App) Start() {
	app.mu.Lock()
	if app.closing {
		app.mu.Unlock()
		return
	}
	if app.cancel != nil {
		app.cancel()
	}
	app.ready = false
	cfg := app.cfg
	app.connection = "Connecting..."
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
	report := func(connection string, ready bool) {
		app.mu.Lock()
		if ctx.Err() != nil || app.cfg.Engine != cfg.Engine {
			app.mu.Unlock()
			return
		}
		app.connection, app.ready = connection, ready
		notify := app.notify
		app.mu.Unlock()
		if notify != nil {
			notify()
		}
	}
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		if ctx.Err() != nil {
			return
		}
		if err := serveDirect(ctx, cfg, app.Log, func(name string) {
			report(name+" ready on 127.0.0.1:48036", true)
		}); err != nil && !errors.Is(err, context.Canceled) {
			report("Connection failed", false)
			app.Log("Host stopped: %v", err)
		}
	}()
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lichess-engine-host.json"), nil
}

func loadConfig(path string) (Config, error) {
	cfg := Config{Host: "https://lichess.org"}
	data, err := os.ReadFile(path)
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
