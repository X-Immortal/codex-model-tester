#!/usr/bin/env bash
# Builds the macOS menu bar application bundle, and optionally a .dmg.
#
#   scripts/build-macos-app.sh              # universal .app + .dmg in dist/
#   VERSION=1.2.3 scripts/build-macos-app.sh
#   ARCHS=arm64 SKIP_DMG=1 scripts/build-macos-app.sh
#
# Only tools shipped with macOS and the Go toolchain are required.
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: this script builds an .app bundle and must run on macOS" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

APP_NAME="${APP_NAME:-Codex Model Tester}"
BUNDLE_ID="${BUNDLE_ID:-io.github.x-immortal.codex-model-tester}"
EXECUTABLE="${EXECUTABLE:-codex-model-tester}"
ARCHS="${ARCHS:-arm64 amd64}"
SKIP_DMG="${SKIP_DMG:-0}"
SOURCE_ICON="${SOURCE_ICON:-cmd/api/tray_icon.png}"
DIST="$ROOT/dist"
APP="$DIST/$APP_NAME.app"

VERSION="${VERSION:-}"
if [[ -z "$VERSION" ]]; then
  VERSION="$(git describe --tags --abbrev=0 2>/dev/null || echo 0.0.0)"
fi
VERSION="${VERSION#v}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> Building $APP_NAME $VERSION for: $ARCHS"
slices=()
for arch in $ARCHS; do
  echo "    go build darwin/$arch"
  GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
    -o "$WORK/$EXECUTABLE-$arch" ./cmd/api
  slices+=("$WORK/$EXECUTABLE-$arch")
done

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

if [[ ${#slices[@]} -gt 1 ]]; then
  echo "==> Merging universal binary"
  lipo -create -output "$APP/Contents/MacOS/$EXECUTABLE" "${slices[@]}"
else
  cp "${slices[0]}" "$APP/Contents/MacOS/$EXECUTABLE"
fi
chmod 755 "$APP/Contents/MacOS/$EXECUTABLE"

echo "==> Generating AppIcon.icns from $SOURCE_ICON"
# sips performs the PNG-to-ICNS conversion using the installed macOS icon
# encoder. This is more reliable across macOS releases than constructing an
# iconset manually and passing it through iconutil.
sips -s format icns "$SOURCE_ICON" --out "$APP/Contents/Resources/AppIcon.icns" >/dev/null

echo "==> Writing Info.plist"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>$APP_NAME</string>
  <key>CFBundleDisplayName</key><string>$APP_NAME</string>
  <key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
  <key>CFBundleExecutable</key><string>$EXECUTABLE</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSUIElement</key><false/>
  <key>NSHumanReadableCopyright</key><string>MIT licensed. See the bundled NOTICE.</string>
</dict>
</plist>
PLIST

printf 'APPL????' > "$APP/Contents/PkgInfo"
cp LICENSE "$APP/Contents/Resources/LICENSE.txt"
cp NOTICE "$APP/Contents/Resources/NOTICE.txt"

echo "==> Signing (ad-hoc)"
codesign --force --sign - --timestamp=none "$APP"
codesign --verify --strict "$APP"

echo "==> Built $APP"

if [[ "$SKIP_DMG" == "1" ]]; then
  exit 0
fi

DMG_ARCH="universal"
if [[ ${#slices[@]} -eq 1 ]]; then
  DMG_ARCH="${ARCHS// /}"
fi
DMG="$DIST/Codex-Model-Tester-macos-$DMG_ARCH.dmg"

echo "==> Packaging $DMG"
STAGE="$WORK/dmg"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
rm -f "$DMG"
hdiutil create -volname "$APP_NAME" -srcfolder "$STAGE" -ov -format UDZO -quiet "$DMG"
codesign --force --sign - "$DMG"

echo "==> Built $DMG"
