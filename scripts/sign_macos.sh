#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
version="${1:?Release version required}"
arch="${2:?Release architecture required}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ "$arch" == arm64 || "$arch" == amd64 ]]
: "${MACOS_CERT_P12_BASE64:?Signing certificate required}"
: "${MACOS_CERT_PASSWORD:?Certificate password required}"
: "${MACOS_SIGNING_IDENTITY:?Developer ID identity required}"
: "${APPLE_NOTARY_KEY_BASE64:?Notarization API key required}"
: "${APPLE_NOTARY_KEY_ID:?Notarization key ID required}"
: "${APPLE_NOTARY_ISSUER_ID:?Notarization issuer required}"
crew_sign_dir="$(mktemp -d)"
crew_sign_keychain="$crew_sign_dir/crew-signing.keychain-db"
crew_sign_password="$(openssl rand -hex 24)"
cleanup() {
  security delete-keychain "$crew_sign_keychain" >/dev/null 2>&1 || true
  rm -rf "$crew_sign_dir"
}
trap cleanup EXIT
export CREW_SIGN_DIR="$crew_sign_dir"
python3 - <<'PY'
import base64, os
from pathlib import Path
root = Path(os.environ['CREW_SIGN_DIR'])
for variable, name in [('MACOS_CERT_P12_BASE64', 'certificate.p12'), ('APPLE_NOTARY_KEY_BASE64', 'notary.p8')]:
    path = root / name
    path.write_bytes(base64.b64decode(os.environ[variable], validate=True))
    path.chmod(0o600)
PY
security create-keychain -p "$crew_sign_password" "$crew_sign_keychain"
security set-keychain-settings -lut 21600 "$crew_sign_keychain"
security unlock-keychain -p "$crew_sign_password" "$crew_sign_keychain"
security import "$crew_sign_dir/certificate.p12" -k "$crew_sign_keychain" -P "$MACOS_CERT_PASSWORD" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple: -s -k "$crew_sign_password" "$crew_sign_keychain"
archive="dist/crew_${version}_darwin_${arch}.tar.gz"
mkdir "$crew_sign_dir/package"
tar -xzf "$archive" -C "$crew_sign_dir/package"
codesign --force --options runtime --timestamp --keychain "$crew_sign_keychain" --sign "$MACOS_SIGNING_IDENTITY" "$crew_sign_dir/package/crew"
codesign --verify --strict --verbose=2 "$crew_sign_dir/package/crew"
ditto -c -k --keepParent "$crew_sign_dir/package/crew" "$crew_sign_dir/notarization.zip"
xcrun notarytool submit "$crew_sign_dir/notarization.zip" --key "$crew_sign_dir/notary.p8" --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID" --wait --timeout 15m
# The notarized Mach-O is unchanged after submission. Its ticket is retrieved
# online by Gatekeeper; standalone CLI binaries do not support staple tickets.
tar -czf "$archive" -C "$crew_sign_dir/package" crew LICENSE NOTICE THIRD_PARTY_LICENSES.txt
