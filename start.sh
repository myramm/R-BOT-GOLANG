#!/usr/bin/env bash
set -u

# Jalankan dari root repository meskipun script dipanggil lewat path lain.
ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR" || exit 1

BINARY="${RBOT_BINARY:-$ROOT_DIR/rbot}"
RESTART_DELAY="${RBOT_RESTART_DELAY:-2}"

if [[ ! -f config.json ]]; then
  echo "[start] config.json belum ada. Salin config.example.json lalu isi credential lokal." >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "[start] Go tidak ditemukan di PATH." >&2
  exit 1
fi

build() {
  echo "[start] build binary..."
  go build -o "$BINARY" .
}

if ! build; then
  echo "[start] build gagal; bot tidak dijalankan." >&2
  exit 1
fi

# Foreground supervisor: cocok untuk tmux/screen/systemd.
# 0       = shutdown normal, jangan loop.
# 128+    = dihentikan signal, jangan loop.
# lainnya = crash/error, build ulang lalu restart setelah jeda.
while true; do
  echo "[start] menjalankan $BINARY"
  "$BINARY"
  status=$?

  if [[ "$status" -eq 0 || "$status" -ge 128 ]]; then
    echo "[start] bot berhenti (exit $status)."
    exit "$status"
  fi

  echo "[start] bot berhenti karena error (exit $status); restart dalam ${RESTART_DELAY}s..." >&2
  sleep "$RESTART_DELAY"
  if ! build; then
    echo "[start] build ulang gagal; retry dalam ${RESTART_DELAY}s..." >&2
    sleep "$RESTART_DELAY"
    continue
  fi
done
