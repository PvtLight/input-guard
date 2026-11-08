#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p build/module-cache 'build/Input Guard.app/Contents/MacOS'
mkdir -p 'build/Input Guard.app/Contents/Resources'
xcrun swiftc -swift-version 5 -O -warnings-as-errors \
  -module-cache-path "$PWD/build/module-cache" \
  Sources/Brand.swift Design/GenerateIcon.swift -o build/generate-icon -framework AppKit
build/generate-icon build/InputGuard.iconset
iconutil -c icns build/InputGuard.iconset -o 'build/Input Guard.app/Contents/Resources/InputGuard.icns'
xcrun swiftc -swift-version 5 -O -warnings-as-errors \
  -module-cache-path "$PWD/build/module-cache" \
  -target arm64-apple-macosx14.0 \
  Sources/Brand.swift Sources/Core.swift Sources/RecoveryClock.swift Sources/RecoverySupervisor.swift Sources/GlobalHotKey.swift Sources/SessionResetObserver.swift Sources/TouchShield.swift Sources/App.swift \
  -o 'build/Input Guard.app/Contents/MacOS/InputGuard' \
  -framework AppKit -framework ApplicationServices -framework Security \
  -framework LocalAuthentication -framework IOKit -framework Carbon
xcrun swiftc -swift-version 5 -O -warnings-as-errors \
  -module-cache-path "$PWD/build/module-cache" \
  -target arm64-apple-macosx14.0 \
  Sources/RecoveryClock.swift Sources/RecoveryMain.swift \
  -o 'build/Input Guard.app/Contents/MacOS/InputGuardRecovery'
cp Info.plist 'build/Input Guard.app/Contents/Info.plist'
codesign --force --sign - --identifier local.kai.inputguard.recovery 'build/Input Guard.app/Contents/MacOS/InputGuardRecovery'
codesign --force --sign - --identifier local.kai.inputguard 'build/Input Guard.app'
codesign --verify --strict 'build/Input Guard.app'
'build/Input Guard.app/Contents/MacOS/InputGuard' --self-test
'build/Input Guard.app/Contents/MacOS/InputGuard' --recovery-tests
printf 'Built: %s/build/Input Guard.app\n' "$PWD"
