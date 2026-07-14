#!/usr/bin/env bash
# Install a prebuilt podswitch release, or build native binaries with Go.
#
#   ./install.sh coordinator
#   ./install.sh agent coordinator.example:8090 AA:BB:CC:DD:EE:FF
#   ./install.sh agent coordinator.example:8090 AA:BB:CC:DD:EE:FF --source go
set -euo pipefail

REPOSITORY="${PODSWITCH_REPOSITORY:-Ozdotdotdot/podswitch}"
SOURCE="release"
VERSION="${PODSWITCH_VERSION:-}"

usage() {
  cat <<'EOF'
usage: install.sh <coordinator|agent> [coordinator-host:port] [headset-mac] [options]

options:
  --source release|go   Install a GitHub Release archive, or build with Go.
  --version TAG         Release tag or Go module version. Defaults to latest.
EOF
}

ROLE="${1:-}"
if [[ -z "$ROLE" ]]; then usage >&2; exit 1; fi
shift
if [[ "$ROLE" != "coordinator" && "$ROLE" != "agent" ]]; then
  echo "error: role must be coordinator or agent" >&2
  exit 1
fi

COORDINATOR_ADDR=""
AIRPODS_MAC=""
if [[ "$ROLE" == "agent" ]]; then
  COORDINATOR_ADDR="${1:-}"
  if [[ -z "$COORDINATOR_ADDR" || "$COORDINATOR_ADDR" == --* ]]; then
    echo "error: agent role requires coordinator-host:port" >&2
    exit 1
  fi
  shift
  if [[ $# -gt 0 && "$1" != --* ]]; then
    AIRPODS_MAC="$1"
    shift
  fi
  AIRPODS_MAC="${PODSWITCH_AIRPODS_MAC:-$AIRPODS_MAC}"
  if [[ -z "$AIRPODS_MAC" ]]; then
    echo "error: agent role requires a headset MAC or PODSWITCH_AIRPODS_MAC" >&2
    exit 1
  fi
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source) SOURCE="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown option $1" >&2; usage >&2; exit 1 ;;
  esac
done
if [[ "$SOURCE" != "release" && "$SOURCE" != "go" ]]; then
  echo "error: source must be release or go" >&2
  exit 1
fi

case "$(uname -s)" in
  Linux) OS="linux" ;;
  *) echo "error: only Linux is supported" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

TMP=""
cleanup() { [[ -z "$TMP" ]] || rm -rf "$TMP"; }
trap cleanup EXIT

if [[ "$SOURCE" == "release" ]]; then
  ASSET="podswitch_${OS}_${ARCH}.tar.gz"
  TMP="$(mktemp -d)"
  if [[ -n "$VERSION" ]]; then
    URL="https://github.com/$REPOSITORY/releases/download/$VERSION/$ASSET"
  else
    URL="https://github.com/$REPOSITORY/releases/latest/download/$ASSET"
  fi
  echo "==> Downloading $URL"
  curl --fail --location --silent --show-error "$URL" --output "$TMP/$ASSET"
  tar -xzf "$TMP/$ASSET" -C "$TMP"
  PAYLOAD="$TMP/podswitch_${OS}_${ARCH}"
  PODSWITCHD="$PAYLOAD/podswitchd"
  PODSWITCH="$PAYLOAD/podswitch"
else
  command -v go >/dev/null || { echo "error: Go is required for --source go" >&2; exit 1; }
  GO_VERSION="${VERSION:-latest}"
  echo "==> Building native binaries with Go ($GO_VERSION)"
  go install "github.com/Ozdotdotdot/podswitch/cmd/podswitchd@$GO_VERSION"
  go install "github.com/Ozdotdotdot/podswitch/cmd/podswitch@$GO_VERSION"
  GOBIN="$(go env GOBIN)"
  if [[ -z "$GOBIN" ]]; then GOBIN="$(go env GOPATH)/bin"; fi
  PODSWITCHD="$GOBIN/podswitchd"
  PODSWITCH="$GOBIN/podswitch"
fi

install -d "$HOME/.local/bin" "$HOME/.config/systemd/user"
echo "==> Installing podswitchd and podswitch in $HOME/.local/bin"
install -m 0755 "$PODSWITCHD" "$HOME/.local/bin/podswitchd"
install -m 0755 "$PODSWITCH" "$HOME/.local/bin/podswitch"

if [[ "$ROLE" == "agent" ]]; then
  install -d "$HOME/.config/podswitch"
  cat > "$HOME/.config/podswitch/agent.env" <<EOF
COORDINATOR=$COORDINATOR_ADDR
HOST=$(hostname)
PODSWITCH_AIRPODS_MAC=$AIRPODS_MAC
EOF
  cat > "$HOME/.config/systemd/user/podswitch-agent.service" <<'EOF'
[Unit]
Description=podswitch agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%h/.config/podswitch/agent.env
ExecStart=%h/.local/bin/podswitchd -mode agent -coordinator ${COORDINATOR} -host ${HOST}
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
EOF
else
  cat > "$HOME/.config/systemd/user/podswitch-coordinator.service" <<'EOF'
[Unit]
Description=podswitch coordinator
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/podswitchd -mode coordinator -addr :8090
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
EOF
fi

UNIT="podswitch-$ROLE.service"
loginctl enable-linger "$USER" || true
systemctl --user daemon-reload
systemctl --user enable --now "$UNIT"
echo "==> Installed and started $UNIT"
systemctl --user --no-pager --full status "$UNIT" | sed -n '1,8p' || true
