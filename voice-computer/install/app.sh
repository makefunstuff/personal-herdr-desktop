#!/bin/sh
# Builds "Herdr Voice.app" in ~/Applications: the app macOS asks for the microphone (once) and lists
# under System Settings → Privacy & Security → Microphone. vc start launches vcd through it. Ad-hoc signed;
# it never touches Herdr.app or the privacy database. Run again after changing cmd/herdr-voice only.
set -e
cd "$(dirname "$0")/.."
APP="$HOME/Applications/Herdr Voice.app"
mkdir -p "$APP/Contents/MacOS"
go build -o "$APP/Contents/MacOS/herdr-voice" ./cmd/herdr-voice
cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>dev.herdr.voice-computer</string>
	<key>CFBundleName</key><string>Herdr Voice</string>
	<key>CFBundleDisplayName</key><string>Herdr Voice</string>
	<key>CFBundleExecutable</key><string>herdr-voice</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>0.1</string>
	<key>CFBundleVersion</key><string>1</string>
	<key>LSMinimumSystemVersion</key><string>13.0</string>
	<key>LSUIElement</key><true/>
	<key>NSMicrophoneUsageDescription</key><string>Herdr Voice listens while you hold the talk key.</string>
</dict>
</plist>
PLIST
codesign -s - --force --identifier dev.herdr.voice-computer "$APP"
codesign -dv "$APP" 2>&1 | grep -E "Identifier|Signature"
echo "built $APP"
