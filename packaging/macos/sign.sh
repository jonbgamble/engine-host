#!/bin/bash
set -euo pipefail

: "${MACOS_CERTIFICATE_BASE64:?Missing Developer ID certificate}"
: "${MACOS_CERTIFICATE_PASSWORD:?Missing certificate password}"
: "${MACOS_SIGN_IDENTITY:?Missing Developer ID Application identity}"
: "${APPLE_ID:?Missing Apple ID}"
: "${APPLE_TEAM_ID:?Missing Apple team ID}"
: "${APPLE_APP_PASSWORD:?Missing app-specific password}"
: "${RUNNER_TEMP:?Missing runner temporary directory}"
: "${RELEASE_TAG:?Missing release tag}"
: "${BUILD_NUMBER:?Missing build number}"

version="${RELEASE_TAG#v}"
version="${version%%[-+]*}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf 'Expected a release tag such as v1.2.3 or v1.2.3-rc.1\n' >&2
  exit 1
fi

workdir="$(mktemp -d "$RUNNER_TEMP/macos-signing.XXXXXX")"
keychain="$workdir/signing.keychain-db"
cleanup() {
  security delete-keychain "$keychain" >/dev/null 2>&1 || true
  rm -rf "$workdir"
}
trap cleanup EXIT

keychain_password="$(openssl rand -base64 32)"
printf '%s' "$MACOS_CERTIFICATE_BASE64" | base64 --decode > "$workdir/certificate.p12"
security create-keychain -p "$keychain_password" "$keychain"
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$keychain_password" "$keychain"
security import "$workdir/certificate.p12" -k "$keychain" \
  -P "$MACOS_CERTIFICATE_PASSWORD" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: \
  -s -k "$keychain_password" "$keychain" >/dev/null

app="build/Direct Engine Host.app"
plist="$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set :CFBundlePackageType APPL' "$plist"
/usr/libexec/PlistBuddy -c "Add :CFBundleShortVersionString string $version" "$plist"
/usr/libexec/PlistBuddy -c "Add :CFBundleVersion string $BUILD_NUMBER" "$plist"
codesign --force --options runtime --timestamp --keychain "$keychain" \
  --sign "$MACOS_SIGN_IDENTITY" "$app"
codesign --verify --deep --strict --verbose=2 "$app"

ditto -c -k --sequesterRsrc --keepParent "$app" "$workdir/notarization.zip"
xcrun notarytool submit "$workdir/notarization.zip" \
  --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" \
  --password "$APPLE_APP_PASSWORD" --wait --timeout 30m \
  --output-format json > "$workdir/notarization.json"
if [[ "$(plutil -extract status raw -o - "$workdir/notarization.json")" != Accepted ]]; then
  cat "$workdir/notarization.json" >&2
  exit 1
fi
xcrun stapler staple "$app"
xcrun stapler validate "$app"
spctl --assess --type execute --verbose=2 "$app"