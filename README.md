# Direct Engine Host

This app connects Lichess analysis pages to a native UCI chess engine running on your device. Your browser connects to Direct Engine Host without any hops or intermediary server. Stocfish 19 through Direct Engine host runs standard fishnet mainline analysis over 10x faster than traditional "external engine" due to reduced networking overhead.

Only Chromium and Firefox are supported at this time.

## Install Go

Use Go 1.26 or newer from the [official downloads](https://go.dev/dl/).

- **macOS:** Run the macOS `.pkg` installer, choosing ARM64 for Apple Silicon or AMD64 for Intel.
- **Linux:** Follow the [official Linux installation steps](https://go.dev/doc/install), using the tarball for your architecture and adding `/usr/local/go/bin` to your `PATH`.
- **Windows:** Run the Windows `.msi` installer, which adds Go to your `PATH` automatically.

Open a new terminal and run `go version` to verify installation.

## Run

```sh
git clone https://github.com/jonbgamble/engine-host
cd ./engine-host
go run .
```

Click **Choose Engine** and select your UCI engine executable. The selected engine starts automatically on subsequent launches. Its name, variants, and thread/hash limits come from UCI, not configuration.

In Lichess analysis, click **Setup direct engine** in the engine selector popup and allow local network access when your browser asks. 

The configurable thread limit is capped at your host's logical CPU count (including SMT), and the hash limit at currently available RAM minus 256 MB, with a 16 MB floor.

That's all you need to know to use this software. Now go nuts and analyse all the positions.

## Configuration

This app is zeroconf, but if you want to tweak the origin it allows connections from to or add custom UCI options, you may do so.

The app stores `direct-engine-host.json` in the OS user configuration directory, importing existing settings on first launch. The supported fields are:

- `engine`: local UCI executable path, selected in the app.
- `host`: permitted browser origin, defaulting to `https://lichess.org`; use your local Lila origin for development.
- `setoption`: optional UCI name/value pairs sent when an engine starts.

```json
{
	"engine": "/path/to/stockfish",
	"host": "https://lichess.org",
	"setoption": [["SyzygyPath", "/path/to/tablebases"]]
}
```

## Browser connection

Direct Engine Host listens only on `127.0.0.1:48036`, using plain HTTP and WebSocket:

- `GET /v1/info` returns engine metadata and protocol version `1`.
- `WS /v1/uci` exchanges one UCI command or output line per text message.

## macOS

Build an unsigned macOS app:

```sh
make macos
open "build/Direct Engine Host.app"
```

On macOS, the host prevents App Nap while running (this does not prevent sleep).

Safari support would probably require dicking around with Keychain Access or a signed Safari extension. That is not implemented because Apple.

Download Firefox if you want to run this native engine on mac.

## Linux

```sh
make install-linux
```

This installs the app for your user account and adds **Direct Engine Host** to the application menu.

## Windows

Untested. Let me know if it works!
