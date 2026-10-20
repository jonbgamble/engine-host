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
original_keychains=()
search_list_changed=false
cleanup() {
  if [[ "$search_list_changed" == true ]]; then
    security list-keychains -d user -s "${original_keychains[@]}" >/dev/null 2>&1 || true
  fi
  security delete-keychain "$keychain" >/dev/null 2>&1 || true
  rm -rf "$workdir"
}
trap cleanup EXIT

security list-keychains -d user > "$workdir/keychains.txt"
while IFS= read -r original_keychain; do
  original_keychain="${original_keychain#*\"}"
  original_keychain="${original_keychain%\"*}"
  original_keychains+=("$original_keychain")
done < "$workdir/keychains.txt"

keychain_password="$(openssl rand -base64 32)"
printf '%s' "$MACOS_CERTIFICATE_BASE64" | base64 --decode > "$workdir/certificate.p12"
security create-keychain -p "$keychain_password" "$keychain"
security list-keychains -d user -s "$keychain" "${original_keychains[@]}"
search_list_changed=true
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$keychain_password" "$keychain"
curl --fail --silent --show-error --location --retry 3 \
  https://www.apple.com/certificateauthority/DeveloperIDG2CA.cer \
  -o "$workdir/developer-id-g2.cer"
security import "$workdir/developer-id-g2.cer" -k "$keychain"
security import "$workdir/certificate.p12" -k "$keychain" \
  -P "$MACOS_CERTIFICATE_PASSWORD" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: \
  -s -k "$keychain_password" "$keychain" >/dev/null

security find-identity -v -p codesigning "$keychain" > "$workdir/identities.txt"
cat "$workdir/identities.txt"
if ! grep -Eq '^[[:space:]]*[0-9]+\) [[:xdigit:]]{40} ' "$workdir/identities.txt"; then
  printf '%s\n' \
    'No valid code-signing identity is available in the imported keychain.' \
    'Check that the .p12 contains an unexpired Developer ID Application certificate and its matching private key.' \
    'An incomplete or untrusted Apple certificate chain can also invalidate the identity.' >&2
  security find-identity -p codesigning "$keychain"
  exit 1
fi
if ! sign_identity="$(awk -v identity="$MACOS_SIGN_IDENTITY" '
  /^[[:space:]]*[0-9]+\) [[:xdigit:]]+ / {
    fingerprint = $2
    name = $0
    sub(/^[^"]*"/, "", name)
    sub(/"[[:space:]]*$/, "", name)
    if (name == identity || toupper(fingerprint) == toupper(identity)) {
      print fingerprint
      found = 1
      exit
    }
  }
  END { exit !found }
' "$workdir/identities.txt")"; then
  printf '%s\n' \
    'MACOS_SIGN_IDENTITY does not match an imported valid identity.' \
    'Set it to the exact quoted certificate name above (without the quotes), or its 40-character SHA-1 fingerprint.' >&2
  exit 1
fi

app="build/Direct Engine Host.app"
plist="$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set :CFBundlePackageType APPL' "$plist"
/usr/libexec/PlistBuddy -c "Add :CFBundleShortVersionString string $version" "$plist"
/usr/libexec/PlistBuddy -c "Add :CFBundleVersion string $BUILD_NUMBER" "$plist"
codesign --force --options runtime --timestamp --keychain "$keychain" \
  --sign "$sign_identity" "$app"
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