#!/usr/bin/env bash
# Install a prebuilt podswitch release, or build native binaries with Go.
#
#   ./install.sh                                      # interactive setup
#   ./install.sh coordinator
#   ./install.sh agent coordinator.example:8090 AA:BB:CC:DD:EE:FF
#   ./install.sh agent --source go                    # interactive agent setup
set -euo pipefail

REPOSITORY="${PODSWITCH_REPOSITORY:-Ozdotdotdot/podswitch}"
SOURCE="release"
VERSION="${PODSWITCH_VERSION:-}"

usage() {
  cat <<'EOF'
usage: install.sh [coordinator|agent] [coordinator-host:port] [headset-mac] [options]

With no role, the installer prompts for coordinator or agent setup. Agent
setup can select from paired Bluetooth devices, with AirPods listed first.

options:
  --source release|go   Install a GitHub Release archive, or build with Go.
  --version TAG         Release tag or Go module version. Defaults to latest.
EOF
}

read_tty() {
  local prompt="$1"
  local value=""
  if [[ ! -r /dev/tty ]]; then
    echo "error: interactive setup needs a terminal. Pass role and agent values as arguments." >&2
    exit 1
  fi
  printf '%s' "$prompt" >/dev/tty
  IFS= read -r value </dev/tty
  REPLY="$value"
}

choose_role() {
  while true; do
    printf '%s\n' "podswitch setup" >/dev/tty
    printf '%s\n' "  1. Coordinator, the always-on handoff server" >/dev/tty
    printf '%s\n' "  2. Agent, a machine that connects to the headphones" >/dev/tty
    read_tty "Choose [1-2]: "
    case "$REPLY" in
      1) ROLE="coordinator"; return ;;
      2) ROLE="agent"; return ;;
      *) echo "Please enter 1 or 2." >/dev/tty ;;
    esac
  done
}

valid_mac() {
  [[ "$1" =~ ^([[:xdigit:]]{2}:){5}[[:xdigit:]]{2}$ ]]
}

choose_headset() {
  local line kind mac name index selection
  local -a macs=() names=() ordered=()
  if command -v bluetoothctl >/dev/null; then
    while IFS= read -r line; do
      read -r kind mac name <<<"$line"
      if [[ "$kind" == "Device" ]] && valid_mac "$mac"; then
        macs+=("$mac")
        names+=("${name:-Unnamed device}")
      fi
    done < <(bluetoothctl devices Paired 2>/dev/null || true)
  fi

  for index in "${!macs[@]}"; do
    if [[ "${names[$index],,}" == *airpods* ]]; then ordered+=("$index"); fi
  done
  for index in "${!macs[@]}"; do
    if [[ "${names[$index],,}" != *airpods* ]]; then ordered+=("$index"); fi
  done

  if [[ ${#ordered[@]} -gt 0 ]]; then
    printf '%s\n' "Paired Bluetooth devices:" >/dev/tty
    for index in "${!ordered[@]}"; do
      selection="${ordered[$index]}"
      printf '  %d. %s (%s)\n' "$((index + 1))" "${names[$selection]}" "${macs[$selection]}" >/dev/tty
    done
    printf '%s\n' "  m. Enter a MAC address manually" >/dev/tty
    while true; do
      read_tty "Headset [1-${#ordered[@]} or m]: "
      if [[ "$REPLY" == "m" || "$REPLY" == "M" ]]; then break; fi
      if [[ "$REPLY" =~ ^[0-9]+$ ]] && (( REPLY >= 1 && REPLY <= ${#ordered[@]} )); then
        AIRPODS_MAC="${macs[${ordered[$((REPLY - 1))]}]}"
        return
      fi
      echo "Please choose a listed device or m." >/dev/tty
    done
  else
    echo "No paired Bluetooth devices found. Enter the headset MAC manually." >/dev/tty
  fi

  while true; do
    read_tty "Headset MAC (AA:BB:CC:DD:EE:FF): "
    if valid_mac "$REPLY"; then
      AIRPODS_MAC="${REPLY^^}"
      return
    fi
    echo "That is not a valid Bluetooth MAC address." >/dev/tty
  done
}

ROLE="${1:-}"
if [[ "$ROLE" == "-h" || "$ROLE" == "--help" ]]; then usage; exit 0; fi
if [[ -z "$ROLE" ]]; then
  choose_role
else
  if [[ "$ROLE" != "coordinator" && "$ROLE" != "agent" ]]; then
    echo "error: role must be coordinator or agent" >&2
    exit 1
  fi
  shift
fi

COORDINATOR_ADDR=""
AIRPODS_MAC=""
if [[ "$ROLE" == "agent" ]]; then
  if [[ $# -gt 0 && "$1" != --* ]]; then
    COORDINATOR_ADDR="$1"
    shift
  fi
  if [[ $# -gt 0 && "$1" != --* ]]; then
    AIRPODS_MAC="$1"
    shift
  fi
  AIRPODS_MAC="${PODSWITCH_AIRPODS_MAC:-$AIRPODS_MAC}"
  if [[ -z "$AIRPODS_MAC" ]]; then
    choose_headset
  fi
  if ! valid_mac "$AIRPODS_MAC"; then
    echo "error: headset MAC must look like AA:BB:CC:DD:EE:FF" >&2
    exit 1
  fi
  AIRPODS_MAC="${AIRPODS_MAC^^}"
  if [[ -z "$COORDINATOR_ADDR" && -r /dev/tty ]]; then
    read_tty "Coordinator address, leave blank for LAN mDNS discovery: "
    COORDINATOR_ADDR="$REPLY"
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
PODSWITCH_COORDINATOR=$COORDINATOR_ADDR
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
ExecStart=%h/.local/bin/podswitchd -mode agent -host ${HOST}
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
