#!/usr/bin/env bash
# Build script for Aux: sets up and builds the frontend and the Go backend.
#
#   ./build.sh              # build everything (frontend + backend)
#   ./build.sh frontend     # npm ci (if needed) + vite build
#   ./build.sh backend      # go build -> bin/aux
#   ./build.sh test         # gofmt check, go vet, go test, frontend tsc
#   ./build.sh clean        # remove build outputs
set -euo pipefail

cd "$(dirname "$0")"

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

frontend() {
  echo "==> frontend (vite)"
  pushd frontend >/dev/null
  if [ ! -d node_modules ]; then
    npm ci --no-audit --no-fund
  fi
  npm run build
  popd >/dev/null
}

backend() {
  echo "==> backend (go, version ${VERSION})"
  mkdir -p bin
  CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o bin/aux ./cmd/server
  echo "    built bin/aux"
}

test_all() {
  echo "==> gofmt"
  fmt_out="$(gofmt -l cmd internal)"
  if [ -n "$fmt_out" ]; then
    echo "gofmt needed for:" && echo "$fmt_out" && exit 1
  fi
  echo "==> go vet"
  go vet ./...
  echo "==> go test"
  go test ./...
  echo "==> frontend typecheck + build"
  frontend
}

clean() {
  echo "==> clean"
  rm -rf bin frontend/dist
}

case "${1:-all}" in
  all)      frontend; backend
            echo
            echo "Done. Run it with: ./bin/aux serve  (frontend is served from frontend/dist)" ;;
  frontend) frontend ;;
  backend)  backend ;;
  test)     test_all ;;
  clean)    clean ;;
  *)        echo "usage: $0 [all|frontend|backend|test|clean]" >&2; exit 2 ;;
esac
