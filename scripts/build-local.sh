#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p build
go tool mygo build -platform darwin/arm64 -skip-dmg
xcodebuild -project Loopwall.xcodeproj -scheme LoopwallExtension \
  -configuration Release -destination 'generic/platform=macOS' \
  -derivedDataPath build/xcode CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO \
  CODE_SIGN_IDENTITY='' DEVELOPMENT_TEAM='' build > build/extension-build.log 2>&1
app='dist/darwin-arm64/Loopwall.app'
extension="$app/Contents/Extensions/LoopwallExtension.appex"
mkdir -p "$app/Contents/Extensions"
ditto 'build/xcode/Build/Products/Release/LoopwallExtension.appex' "$extension"

sign_identity="${MACOS_SIGNING_IDENTITY:--}"
codesign --force --sign "$sign_identity" --entitlements scripts/extension.entitlements "$extension"
cp LICENSE "$app/Contents/Resources/LICENSE-Phosphene.txt"
codesign --force --sign "$sign_identity" "$app"
codesign --verify --deep --strict "$app"

if [ "$sign_identity" != "-" ]; then
  # 配置了固定签名证书时，主应用与扩展的 designated requirement 都必须绑定证书，
  # 不能退回 cdhash，否则每次更新都会重新触发本地网络权限或扩展身份识别。
  verify_dr() {
    local target="$1"
    local expected_id="$2"
    local dr
    dr="$(codesign -d -r- "$target" 2>&1 | grep 'designated =>' || true)"
    echo "$target: $dr"
    case "$dr" in
      *cdhash*|"")
        echo "error: $target is not signed with certificate $sign_identity" >&2
        exit 1
        ;;
    esac
    case "$dr" in
      *"identifier \"$expected_id\""*"certificate"*) ;;
      *)
        echo "error: $target has unexpected designated requirement: $dr" >&2
        exit 1
        ;;
    esac
  }
  verify_dr "$extension" "local.lumiaqian.loopwall.extension"
  verify_dr "$app" "local.lumiaqian.loopwall"
fi

printf '%s\n' "$PWD/$app"
