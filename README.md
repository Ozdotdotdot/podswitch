# podswitch

Cross-device AirPods Max handoff across your Linux boxes (Tailscale-only —
see `DESIGN.md` for why the iPhone/Mac leg is out of scope).

One binary, three roles selected by flag:

```
podswitchd -mode coordinator -addr :8090
podswitchd -mode agent -coordinator <ts-name>:8090 -host <this-host>
```

Agents dial OUT to the coordinator over a persistent WebSocket, so a
sleeping/roaming laptop never needs to be addressed inbound — a dropped
socket just means that host can't be holding the buds.

## CLI

```
podswitchd here    # grab the AirPods onto this host
podswitchd state   # who's online / who's holding them
```

No `-coordinator` needed most of the time. It's resolved in order:

1. `-coordinator <host:port>` flag (one-off override)
2. `PODSWITCH_COORDINATOR` env var
3. cached `~/.config/podswitch/config.toml`
4. mDNS (`_podswitch._tcp`, LAN-only, ~3s) — the coordinator advertises its
   Tailscale address, so once discovered on the home LAN the cached copy
   keeps working when you're away too

Define it once explicitly instead of waiting on mDNS:

```
podswitchd config coordinator 100.64.0.1:8090
```

## Build

```
make build   # native
make arm64   # Pi
make amd64   # x86_64 hosts
```

## Deploy

```
rsync -a deploy/ bin/podswitchd-arm64 user@host:~/podswitch-deploy/
ssh user@host 'cd ~/podswitch-deploy && ./install.sh agent 100.64.0.1:8090'
# or on the switch server:
ssh user@switchserver 'cd ~/podswitch-deploy && ./install.sh coordinator'
```

See `DESIGN.md` for the full design rationale and roadmap (Phase 1 is what's
implemented here; attention-follow and widgets are later phases).
