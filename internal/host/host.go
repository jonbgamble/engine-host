package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Name       string      `json:"name"`
	Engine     string      `json:"engine"`
	Token      string      `json:"token"`
	SetOption  [][2]string `json:"setoption,omitempty"`
	Host       string      `json:"host"`
	Broker     string      `json:"broker"`
	MaxThreads int         `json:"maxThreads"`
	MaxHash    int         `json:"maxHash"`
	KeepAlive  int         `json:"keepAlive"`
}

type App struct {
	mu      sync.Mutex
	path    string
	cfg     Config
	status  []string
	notify  func()
	cancel  context.CancelFunc
	restart *time.Timer
	workers sync.WaitGroup
	closing bool
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
	app.update(func(cfg *Config) { cfg.Engine = path })
	app.Log("Engine: %s", path)
}

func (app *App) Login() error {
	app.Log("Opening Host authorization")
	token, err := oauthLogin(app.Config().Host)
	if err != nil {
		app.Log("Login failed: %v", err)
		return err
	}
	app.update(func(cfg *Config) { cfg.Token = token })
	app.Log("Logged in with Host")
	return nil
}

func (app *App) Start() { app.startHost() }

func (app *App) Close() {
	app.mu.Lock()
	app.closing = true
	if app.restart != nil {
		app.restart.Stop()
	}
	if app.cancel != nil {
		app.cancel()
	}
	app.mu.Unlock()
	app.workers.Wait()
}

func (app *App) update(change func(*Config)) {
	app.mu.Lock()
	change(&app.cfg)
	cfg, path := app.cfg, app.path
	app.mu.Unlock()
	if err := saveConfig(path, cfg); err != nil {
		app.Log("Saving configuration: %v", err)
		return
	}
	app.scheduleHost()
}

func (app *App) scheduleHost() {
	app.mu.Lock()
	if app.restart != nil {
		app.restart.Stop()
	}
	app.restart = time.AfterFunc(500*time.Millisecond, app.startHost)
	app.mu.Unlock()
}

func (app *App) startHost() {
	app.mu.Lock()
	if app.closing {
		app.mu.Unlock()
		return
	}
	if app.cancel != nil {
		app.cancel()
	}
	cfg := app.cfg
	if cfg.Token == "" || cfg.Engine == "" || cfg.Name == "" {
		app.cancel = nil
		app.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.cancel = cancel
	app.workers.Add(1)
	app.mu.Unlock()
	go func() {
		defer app.workers.Done()
		if err := provide(ctx, cfg, app.Log); err != nil && !errors.Is(err, context.Canceled) {
			app.Log("Host stopped: %v", err)
		}
	}()
}

func defaultConfig() Config {
	return Config{
		Name: "Native", Host: "https://lichess.org", Broker: "https://engine.lichess.ovh",
		MaxThreads: runtime.NumCPU(), MaxHash: 512, KeepAlive: 300,
	}
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "uci-engine-host.json"), nil
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
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
