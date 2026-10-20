# Lichess engine host

## Run

```sh
go run .
```

Click **Choose engine** and select a UCI engine executable. Engine-host connects directly to the browser, without a Lichess login or intermediary server. The selected engine starts automatically on subsequent launches. Its name, variants, and thread/hash limits come from UCI, not configuration.

## Browser connection

Engine-host listens only on `127.0.0.1:48036`, using plain HTTP and WebSocket:

- `GET /v1/info` returns engine metadata and protocol version `1`.
- `WS /v1/uci` exchanges one UCI command or output line per text message.

In Lichess analysis, use **Connect local engine** in the engine settings and allow local network access when the browser asks. Chromium and Firefox can use the plain loopback connection. Each socket has its own engine process; starting a search closes the previous searching socket and terminates its engine.

### Safari

Safari support requires a signed Safari extension, which is not implemented yet. There is no TLS listener, certificate generation, or certificate-import setup.

The extension needs to bridge discovery and UCI messages between approved Lichess pages and this local service. Signing alone does not bypass browser network restrictions: extension-side loopback access needs to be verified in Safari, with a native-messaging bridge as an alternative. The macOS app must bundle the extension, and users must enable it and grant website access in Safari. Production distribution also needs Apple signing and the appropriate notarization or App Store workflow.

## Configuration

The app stores `lichess-engine-host.json` in the OS user configuration directory. The supported fields are:

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

Unrecognized fields in existing configuration files are discarded when the app saves its configuration.

## Linux application

```sh
make install-linux
```

This installs the app for your user account and adds **Lichess Engine Host** to the application menu.

## macOS app bundle

Build an unsigned macOS app:

```sh
make macos
open "build/Lichess Engine Host.app"
```

Local unsigned bundles built by you do not require an Apple Developer account. This target builds only engine-host; it does not yet bundle a Safari extension.
