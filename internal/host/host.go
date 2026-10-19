package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Mode string

const (
	ModeDisconnected Mode = "disconnected"
	ModeExternal     Mode = "external"
	ModeDirect       Mode = "direct"
)

type Config struct {
	Name         string      `json:"name"`
	Engine       string      `json:"engine"`
	Mode         Mode        `json:"mode"`
	Token        string      `json:"token,omitempty"`
	TokenStorage string      `json:"tokenStorage,omitempty"`
	SetOption    [][2]string `json:"setoption,omitempty"`
	Host         string      `json:"host"`
	Broker       string      `json:"broker"`
	MaxThreads   int         `json:"maxThreads"`
	MaxHash      int         `json:"maxHash"`
	KeepAlive    int         `json:"keepAlive"`
}

const (
	tokenStorageKeyring   = "keyring"
	tokenStoragePlaintext = "plaintext"
)

type App struct {
	mu           sync.Mutex
	path         string
	cfg          Config
	status       []string
	notify       func()
	cancel       context.CancelFunc
	done         <-chan struct{}
	restart      *time.Timer
	workers      sync.WaitGroup
	closing      bool
	loggingIn    bool
	connection   string
	ready        bool
	callbackURLs <-chan *url.URL
	tokens       tokenStore
}

func New(notify func(), callbackURLs <-chan *url.URL) (*App, error) {
	return newApp(notify, callbackURLs, systemTokenStore{})
}

func newApp(notify func(), callbackURLs <-chan *url.URL, tokens tokenStore) (*App, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	tokenMessage := loadStoredToken(&cfg, tokens, path)
	if err := saveConfig(path, cfg); err != nil {
		return nil, err
	}
	app := &App{
		path: path, cfg: cfg, notify: notify, callbackURLs: callbackURLs,
		tokens: tokens,
	}
	app.Log("Configuration: %s", path)
	if tokenMessage != "" {
		app.Log("%s", tokenMessage)
	}
	return app, nil
}

func loadStoredToken(cfg *Config, tokens tokenStore, path string) string {
	switch cfg.TokenStorage {
	case tokenStorageKeyring:
		token, err := tokens.Get(cfg.Host)
		if err != nil {
			cfg.Token = ""
			return fmt.Sprintf("Login token unavailable from %s; log in again: %v", tokenStoreName(), err)
		}
		if token == "" {
			cfg.Token = ""
			return fmt.Sprintf("Login token unavailable from %s; log in again", tokenStoreName())
		}
		cfg.Token = token
		return "Login token stored in " + tokenStoreName()
	case tokenStoragePlaintext:
		if cfg.Token != "" {
			return "Login token stored in plaintext: " + path
		}
	default:
		cfg.Token = ""
	}
	return ""
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

func (app *App) Connection() (Mode, string) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.cfg.Mode == ModeDisconnected {
		return ModeDisconnected, "Disconnected"
	}
	if app.cfg.Mode == ModeExternal {
		if app.loggingIn {
			return ModeDisconnected, "Waiting for Lichess login"
		}
		if app.cfg.Token == "" {
			return ModeDisconnected, "Login required"
		}
	}
	if app.cfg.Engine == "" {
		return ModeDisconnected, "Choose an engine"
	}
	if app.ready {
		return app.cfg.Mode, app.connection
	}
	return ModeDisconnected, app.connection
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

func (app *App) SetMode(mode Mode) {
	if active, _ := app.Connection(); active == mode && app.Config().Mode == mode {
		return
	}
	app.update(func(cfg *Config) { cfg.Mode = mode })
	app.Log("Mode: %s", mode)
	if mode == ModeExternal && app.Config().Token == "" {
		go app.Login()
	}
}

func (app *App) storeEngineName(enginePath, name string) error {
	app.mu.Lock()
	if app.cfg.Engine != enginePath {
		app.mu.Unlock()
		return context.Canceled
	}
	app.cfg.Name = name
	cfg, path := app.cfg, app.path
	app.mu.Unlock()
	if err := saveConfig(path, cfg); err != nil {
		return err
	}
	app.Log("Engine name: %s", name)
	return nil
}

func (app *App) Login() error {
	app.mu.Lock()
	if app.loggingIn {
		app.mu.Unlock()
		return errors.New("authorization already in progress")
	}
	app.loggingIn = true
	app.mu.Unlock()
	defer func() {
		app.mu.Lock()
		app.loggingIn = false
		notify := app.notify
		app.mu.Unlock()
		if notify != nil {
			notify()
		}
	}()

	app.Log("Opening Host authorization")
	token, err := oauthLogin(app.Config().Host, app.callbackURLs)
	if err != nil {
		app.Log("Login failed: %v", err)
		return err
	}
	if err := app.storeToken(token); err != nil {
		app.Log("Login failed: %v", err)
		return err
	}
	app.Log("Logged in with Host")
	return nil
}

func (app *App) storeToken(token string) error {
	app.mu.Lock()
	host := app.cfg.Host
	app.mu.Unlock()
	storeErr := app.tokens.Set(host, token)

	app.mu.Lock()
	app.cfg.Token = token
	if storeErr == nil {
		app.cfg.TokenStorage = tokenStorageKeyring
	} else {
		app.cfg.TokenStorage = tokenStoragePlaintext
	}
	cfg, path := app.cfg, app.path
	app.mu.Unlock()
	if err := saveConfig(path, cfg); err != nil {
		return fmt.Errorf("save configuration: %w", err)
	}
	if storeErr == nil {
		app.Log("Login token stored in %s", tokenStoreName())
	} else {
		app.Log("%s unavailable: %v", tokenStoreName(), storeErr)
		app.Log("Login token stored in plaintext: %s", path)
	}
	app.scheduleHost()
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
	app.connection, app.ready = "Connecting...", false
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
	app.ready = false
	cfg := app.cfg
	app.connection = fmt.Sprintf("Connecting %s...", cfg.Mode)
	if cfg.Mode == ModeDisconnected || (cfg.Mode == ModeExternal && cfg.Token == "") || cfg.Engine == "" {
		app.cancel = nil
		app.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	previous := app.done
	done := make(chan struct{})
	app.cancel = cancel
	app.done = done
	app.workers.Add(1)
	app.mu.Unlock()
	report := func(connection string, ready bool) {
		app.mu.Lock()
		if ctx.Err() != nil || app.cfg.Mode != cfg.Mode || app.cfg.Engine != cfg.Engine {
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
		defer app.workers.Done()
		defer close(done)
		if previous != nil {
			<-previous
		}
		if ctx.Err() != nil {
			return
		}
		start := provide
		connection := "Connected to Lichess"
		if cfg.Mode == ModeDirect {
			start = serveDirect
			connection = "Listening on 127.0.0.1:48036 / 48037"
		}
		if err := start(ctx, cfg, app.Log, app.storeEngineName, func() { report(connection, true) }); err != nil && !errors.Is(err, context.Canceled) {
			report("Connection failed", false)
			app.Log("Host stopped: %v", err)
		}
	}()
}

func defaultConfig() Config {
	return Config{
		Name: "Native", Mode: ModeDisconnected, Host: "https://lichess.org", Broker: "https://engine.lichess.ovh",
		MaxThreads: runtime.NumCPU(), MaxHash: 512, KeepAlive: 300,
	}
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lichess-engine-host.json"), nil
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
	if cfg.TokenStorage != tokenStoragePlaintext {
		cfg.Token = ""
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
