# Lichess engine host

Clone this repo and:

## All platforms
```
go run .
```

Choose **Disconnected**, **External**, or **Direct** in the mode selector. External uses the brokered OAuth connection and opens Lichess login when needed; Direct connects the browser straight to the selected engine without logging in. You can choose an engine and switch modes at any time.

### Direct mode

Direct mode listens on `127.0.0.1:48036` for HTTP/WS and `127.0.0.1:48037` for HTTPS/WSS. Both ports must be available. In Lichess analysis, use **Connect local engine** in the engine settings if your engine is not already listed, and allow local network access when the browser asks. Discovery tries plain HTTP first, then HTTPS, without browser sniffing.

Safari requires a trusted certificate for a secure local connection. If **Connect local engine** cannot connect, its dialog offers **Open secure setup**. You can also open `http://127.0.0.1:48036/setup` directly. Download the certificate, import it into the login keychain in Keychain Access, and set its **Trust > Secure Sockets Layer (SSL)** policy to **Always Trust**. Close the certificate window and authenticate if macOS asks, check `https://127.0.0.1:48037/setup`, then return to analysis and retry.

Engine-host generates a unique self-signed server certificate and private key in the `.tls` directory beside its configuration file. It reuses them across launches; do not delete them unless you intend to repeat setup. The certificate is valid for five years and covers `127.0.0.1`, `::1`, and `localhost`. Its SHA-256 fingerprint appears on the setup page and in the host activity log. This is a server certificate, not a general-purpose certificate authority. Keychain trust is independent of Safari website data.

Background browser requests cannot distinguish an untrusted certificate from other connection failures, so certificate setup is offered as a possibility, not diagnosed automatically. Browsers that connect over HTTP do not need certificate setup.

### External mode

Raw executables produced by `go build .` and `go run .` use loopback HTTP callbacks for oauth responses. On mac, you'll run into a Safari issue where it doesn't want to open the callback and complete the transaction for security reasons. At this point, it's easiest to just copy the url from the address bar and copy it to another browser. This will deliver the bearer token.

At login, the host tries to store the bearer token in macOS Keychain, Windows Credential Manager, or Secret Service on Linux. If that write fails, it stores the token in its `0600` JSON configuration instead. The app log reports the selected location. That choice remains authoritative until the next login; startup does not probe or migrate between stores.

Even better:

## macOS app bundle (sidestep Safari interstitial issues)

This makes a macOS app bundle you can double click. Build an unsigned app for mac:

```sh
make macos
open "build/Lichess Engine Host.app"
```

The bundle registers `org.lichess.engine-host` as its OAuth callback scheme. Windows and Linux builds continue to use the loopback HTTP callback.

Local unsigned bundles built by you do not require an Apple Developer account.

Find your freshly built Lichess Engine Host in the build folder.
