#!/usr/bin/env bash
# install.sh — deploy podswitch onto a host as a systemd *user* service.
# Run this ON the target, from the repo's deploy/ dir, after copying both
# podswitchd-<arch> and podswitch-<arch> next to this script (arch defaults
# to the host's own uname -m; override with a 3rd arg, e.g. "arm64").
#
#   ./install.sh coordinator                     # switch server
#   ./install.sh agent 100.64.0.1:8090       # laptop / Pi / workstation
#
# Installs the daemon (podswitchd) + CLI (podswitch, if present) + matching
# systemd user unit, enables lingering so it survives logout, and (for
# agent) writes ~/.config/podswitch/agent.env.
set -euo pipefail

ROLE="${1:?usage: install.sh <coordinator|agent> [coordinator-host:port] [arch-suffix]}"
COORDINATOR_ADDR="${2:-}"
HERE="$(cd "$(dirname "$0")" && pwd)"

case "$(uname -m)" in
  aarch64|arm64) DEFAULT_ARCH="arm64" ;;
  *)             DEFAULT_ARCH="amd64" ;;
esac
ARCH="${3:-$DEFAULT_ARCH}"

UNIT_DIR="$HOME/.config/systemd/user"

if [[ "$ROLE" != "coordinator" && "$ROLE" != "agent" ]]; then
  echo "error: role must be 'coordinator' or 'agent'" >&2
  exit 1
fi
if [[ "$ROLE" == "agent" && -z "$COORDINATOR_ADDR" ]]; then
  echo "error: agent role requires coordinator-host:port (e.g. 100.64.0.1:8090)" >&2
  exit 1
fi

mkdir -p "$HOME/.local/bin" "$UNIT_DIR"

echo "==> Installing podswitchd (daemon) -> $HOME/.local/bin/podswitchd"
install -m 0755 "$HERE/podswitchd-$ARCH" "$HOME/.local/bin/podswitchd"

if [[ -f "$HERE/podswitch-$ARCH" ]]; then
  echo "==> Installing podswitch (CLI) -> $HOME/.local/bin/podswitch"
  install -m 0755 "$HERE/podswitch-$ARCH" "$HOME/.local/bin/podswitch"
else
  echo "==> podswitch-$ARCH not found next to this script; skipping CLI install (daemon only)"
fi

UNIT="podswitch-$ROLE.service"
echo "==> Installing systemd user unit: $UNIT"
install -m 0644 "$HERE/$UNIT" "$UNIT_DIR/$UNIT"

if [[ "$ROLE" == "agent" ]]; then
  ENV_DIR="$HOME/.config/podswitch"
  ENV_FILE="$ENV_DIR/agent.env"
  mkdir -p "$ENV_DIR"
  cat > "$ENV_FILE" <<EOF
COORDINATOR=$COORDINATOR_ADDR
HOST=$(hostname)
EOF
  echo "==> Wrote $ENV_FILE (edit HOST there to change the reported identity)"
fi

echo "==> Enabling linger (service survives logout)"
loginctl enable-linger "$USER" || true

echo "==> Reloading + enabling $UNIT"
systemctl --user daemon-reload
systemctl --user enable --now "$UNIT"

echo "==> Done. Status:"
systemctl --user --no-pager status "$UNIT" | head -n 5 || true
