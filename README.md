# Native engine host

Clone this repo and:

## All platforms
```
go run .
```
Raw executables produced by `go build .` and `go run .` use loopback HTTP callbacks for oauth responses. On mac, you'll run into a Safari issue where it doesn't want to open the callback and complete the transaction for security reasons. At this point, it's easiest to just copy the url from the address bar and copy it to another browser. This will deliver the bearer token.

Even better:

## macOS app bundle (sidestep Safari interstitial issues)

This makes a macOS app bundle you can double click. Build an unsigned app for mac:

```sh
make macos
open "build/Native Engine Host.app"
```

The bundle registers `org.lichess.engine-host` as its OAuth callback scheme. Windows and Linux builds continue to use the loopback HTTP callback.

Local unsigned bundles built by you do not require an Apple Developer account.

Find your freshly built Native Engine Host in the build folder.
