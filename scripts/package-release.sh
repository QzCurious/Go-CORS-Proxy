#!/usr/bin/env sh
set -eu

case "$GOOS/$GOARCH" in
  darwin/amd64) platform="macos-intel" ;;
  darwin/arm64) platform="macos-apple-silicon" ;;
  windows/amd64) platform="windows-x64" ;;
  windows/arm64) platform="windows-arm64" ;;
  *)
    echo "unsupported release target: $GOOS/$GOARCH" >&2
    exit 1
    ;;
esac

artifact="seamless-cors-${VERSION}-${platform}"

case "$GOOS" in
  darwin) output=seamless-cors ;;
  windows) output="${artifact}.exe" ;;
esac

CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X seamless-cors/internal/version.Version=$VERSION" \
  -o "$output" \
  ./cmd/seamless-cors

if [ "$GOOS" = darwin ]; then
  tar -czf "${artifact}.tar.gz" "$output"
  rm "$output"
fi
