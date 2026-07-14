# podswitch

Move AirPods Max between Linux machines. A coordinator orchestrates the handoff, and each audio host runs a small agent that controls BlueZ and PipeWire.

Run `podswitch` with no arguments for the interactive picker. It receives an initial state snapshot and every later change through one persistent WebSocket connection. There is no UI polling.

```sh
podswitch                 # interactive picker (p toggles selected host's MPD)
podswitch status          # script-friendly human status
podswitch status --json   # raw coordinator snapshot
podswitch here            # move AirPods to this host
```

## Quick setup

Install the coordinator on one always-on Linux machine, then install an agent on every machine that can use the headphones. The installer downloads a prebuilt release archive for the host architecture. It supports `amd64` and `arm64`, including Raspberry Pi 3 class machines.

### Install order

1. Run the installer on the always-on server and choose **Coordinator**.
2. Run it on every machine that can use the headphones and choose **Agent**.
3. On each agent, select the paired AirPods when prompted. Then run `podswitch` to open the picker.

```sh
# Interactive setup. Choose coordinator or agent, then select a paired
# Bluetooth device. AirPods appear first when present.
curl -fsSL https://github.com/Ozdotdotdot/podswitch/releases/latest/download/install.sh \
  | bash

# Fully non-interactive agent setup also works.
curl -fsSL https://github.com/Ozdotdotdot/podswitch/releases/latest/download/install.sh \
  | bash -s -- agent coordinator-host:8090 AA:BB:CC:DD:EE:FF
```

The installer writes a systemd user service and starts it. Leave the coordinator address blank during interactive agent setup to use LAN mDNS discovery, or enter an address to pin it.

For development, build locally instead:

```sh
./deploy/install.sh agent coordinator-host:8090 AA:BB:CC:DD:EE:FF --source go
```

## Release artifacts

Build the two uploadable release archives and checksums:

```sh
make dist
```

Upload `dist/podswitch_linux_amd64.tar.gz`, `dist/podswitch_linux_arm64.tar.gz`, and `dist/checksums.txt` to a GitHub Release. Also upload `deploy/install.sh` as `install.sh`. Archive names are intentionally stable so the install command can use GitHub's `latest/download` URL.

## Architecture

- `podswitchd` is the daemon. Run it as either a coordinator or agent.
- `podswitch` is the CLI and Bubble Tea client.
- Agents keep an outbound WebSocket to the coordinator. This works for roaming and sleeping laptops without inbound connections.
- The coordinator exposes `GET /api/state`, `POST /api/grab`, `POST /api/toggle`, and `GET /ws/watch`.
- In the interactive picker, `p` toggles MPD playback on the selected online agent. A small note-and-sparkle mark appears beside hosts currently reporting MPD playback. The agent watches MPD with `mpc idle player`, rather than polling it.
- Playback control is optional. It needs the standard `mpc` command configured to reach MPD on that agent; without it, switching the headphones still works and `p` reports the local MPD error.
