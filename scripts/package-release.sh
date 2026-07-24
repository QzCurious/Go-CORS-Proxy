#!/usr/bin/env sh
set -eu

executable=seamless-cors
if [ "$GOOS" = windows ]; then
  executable="$executable.exe"
fi

case "$GOOS/$GOARCH" in
  darwin/amd64) platform="macos-intel" ;;
  darwin/arm64) platform="macos-apple-silicon" ;;
  windows/amd64) platform="windows-x64" ;;
  windows/arm64) platform="windows-arm64" ;;
  *) platform="${GOOS}-${GOARCH}" ;;
esac

distribution="seamless-cors-${VERSION}-${platform}"

mkdir "$distribution"

CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X seamless-cors/internal/version.Version=$VERSION" \
  -o "$distribution/$executable" \
  ./cmd/seamless-cors

zip -r "$distribution.zip" "$distribution"
