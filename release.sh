#!/usr/bin/env bash
set -euo pipefail

APP_NAME="flowlayer-client-tui"
VERSION="1.0.0"
MAIN_PKG="./"
DIST_DIR="dist"
GPG_ID="D3372B726ED237D9780CF0F4E4A9366CF07BC7C8"

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

setup_gpg() {
  export GNUPGHOME
  GNUPGHOME="$(mktemp -d)"
  chmod 700 "$GNUPGHOME"

  if [[ ! -f /gpg/private.asc ]]; then
    echo "Erreur: fichier GPG privé introuvable: /gpg/private.asc" >&2
    exit 1
  fi

  if [[ ! -f /gpg/public.asc ]]; then
    echo "Erreur: fichier GPG public introuvable: /gpg/public.asc" >&2
    exit 1
  fi

  gpg --batch --import /gpg/private.asc >/dev/null 2>&1
  gpg --batch --import /gpg/public.asc >/dev/null 2>&1

  export GPG_TTY="${GPG_TTY:-$(tty 2>/dev/null || true)}"
}

teardown_gpg() {
  if [[ -n "${GNUPGHOME:-}" && -d "${GNUPGHOME:-}" ]]; then
    rm -rf "$GNUPGHOME"
  fi
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

generate_checksums() {
  echo "==> Génération SHA256SUMS"
  (
    cd "$DIST_DIR"
    sha256sum *.tar.gz *.zip > SHA256SUMS
  )
}

print_checksums() {
  echo
  echo "==> SHA256 checksums"
  cat "${DIST_DIR}/SHA256SUMS"
}

sign_checksums() {
  echo "==> Préparation GPG"
  setup_gpg

  echo "==> Signature GPG de SHA256SUMS"
  gpg --batch --yes --local-user "$GPG_ID" --detach-sign --armor "${DIST_DIR}/SHA256SUMS"
  mv "${DIST_DIR}/SHA256SUMS.asc" "${DIST_DIR}/SHA256SUMS.sig"
}

export_public_key() {
  echo "==> Export clé publique GPG"
  gpg --batch --yes --armor --export "$GPG_ID" > "${DIST_DIR}/gpg.key"
}

main() {
  trap teardown_gpg EXIT

  require_cmd go
  require_cmd tar
  require_cmd zip
  require_cmd sha256sum
  require_cmd gpg

  cleanup

  for target in "${TARGETS[@]}"; do
    # shellcheck disable=SC2086
    build_target $target
  done

  generate_checksums
  sign_checksums
  export_public_key

  echo
  echo "Release terminée. Fichiers générés dans ${DIST_DIR}/ :"
  print_checksums
  ls -lh "${DIST_DIR}"
}

main "$@"