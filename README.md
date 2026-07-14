# podswitch

Cross-device AirPods Max handoff across your Linux boxes (Tailscale-only —
see `DESIGN.md` for why the iPhone/Mac leg is out of scope).

One binary, three roles selected by flag:

```
podswitchd -mode coordinator -addr :9090
podswitchd -mode agent -coordinator <ts-name>:9090 -host <this-host>
```

Agents dial OUT to the coordinator over a persistent WebSocket, so a
sleeping/roaming laptop never needs to be addressed inbound — a dropped
socket just means that host can't be holding the buds.

## CLI

```
podswitchd here  -coordinator <ts-name>:9090          # grab the AirPods onto this host
podswitchd state -coordinator <ts-name>:9090           # who's online / who's holding them
```

Or set `PODSWITCH_COORDINATOR=<ts-name>:9090` once and drop the flag.

## Build

```
make build   # native
make arm64   # Pi
make amd64   # x86_64 hosts
```

## Deploy

```
rsync -a deploy/ bin/podswitchd-arm64 user@host:~/podswitch-deploy/
ssh user@host 'cd ~/podswitch-deploy && ./install.sh agent 100.64.0.1:9090'
# or on the switch server:
ssh user@switchserver 'cd ~/podswitch-deploy && ./install.sh coordinator'
```

See `DESIGN.md` for the full design rationale and roadmap (Phase 1 is what's
implemented here; attention-follow and widgets are later phases).
