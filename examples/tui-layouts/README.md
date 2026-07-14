# TUI layout explorations

These are deliberately fake, standalone Bubble Tea programs for choosing a
composition before the real coordinator-backed TUI is implemented.

```sh
go run ./examples/tui-layouts/wide-split
go run ./examples/tui-layouts/corner-picker
go run ./examples/tui-layouts/center-card
```

All three use the same hard-coded hosts and controls: `up`/`down` or `j`/`k`
select a host, `enter` simulates a grab with a small inline pulse, and `q`
quits. Their rotating headset is genuinely rendered from parametric 3D
geometry on every Bubble Tea tick; it is not a sequence of authored frames.

- **wide-split** — animation and host control panel share one horizontal
  workspace. Best when the terminal is reasonably wide.
- **corner-picker** — the animation lives quietly in the top-left while the
  picker is centered in the remaining visual field.
- **center-card** — the compact, centered card treatment, retained as a
  direct comparison against the full-terminal options.
