#!/usr/bin/env bash
set -euo pipefail

APP_NAME="flowlayer-client-tui"
VERSION="1.0.0"
MAIN_PKG="./"
DIST_DIR="dist"

TARGETS=(
  "linux amd64 tar.gz"
  "linux arm64 tar.gz"
  "darwin amd64 tar.gz"
  "darwin arm64 tar.gz"
  "windows amd64 zip"
  "windows arm64 zip"
)

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Erreur: commande manquante: $1" >&2
    exit 1
  }
}

cleanup() {
  rm -rf "$DIST_DIR"
  mkdir -p "$DIST_DIR"
}

display_os_name() {
  local goos="$1"

  case "$goos" in
    darwin)
      echo "macos"
      ;;
    *)
      echo "$goos"
      ;;
  esac
}

build_target() {
  local goos="$1"
  local goarch="$2"
  local archive_format="$3"

  local ext=""
  if [[ "$goos" == "windows" ]]; then
    ext=".exe"
  fi

  local os_label
  os_label="$(display_os_name "$goos")"

  local release_name="${APP_NAME}-${VERSION}-${os_label}-${goarch}"
  local staging_dir="${DIST_DIR}/${release_name}"
  local binary_name="${APP_NAME}${ext}"

  echo "==> Build ${goos}/${goarch}"
  mkdir -p "$staging_dir"

  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -buildvcs=false -ldflags="-s -w" -o "${staging_dir}/${binary_name}" "$MAIN_PKG"

  if [[ "$archive_format" == "tar.gz" ]]; then
    echo "==> Archive ${release_name}.tar.gz"
    tar -C "$DIST_DIR" -czf "${DIST_DIR}/${release_name}.tar.gz" "$release_name"
  else
    echo "==> Archive ${release_name}.zip"
    (
      cd "$DIST_DIR"
      zip -rq "${release_name}.zip" "$release_name"
    )
  fi

  rm -rf "$staging_dir"
}

main() {
  require_cmd go
  require_cmd tar
  require_cmd zip

  cleanup

  for target in "${TARGETS[@]}"; do
    # shellcheck disable=SC2086
    build_target $target
  done

  echo
  echo "Archives générées dans ${DIST_DIR}/ :"
  ls -lh "${DIST_DIR}"
}

main "$@"