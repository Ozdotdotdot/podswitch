# Music indicator explorations

These are Bubble Tea prototypes retained from choosing the compact playback
indicator used by the real MPD controls.

```sh
go run ./examples/music-indicators/waveform
go run ./examples/music-indicators/equalizer
go run ./examples/music-indicators/spinner
go run ./examples/music-indicators/jingle
```

Use arrow keys or `j`/`k` to select a host, `p` to toggle its fake playback,
and `q` to quit. Individual variants choose their own animation cadence.

- **waveform** uses a restrained rolling waveform and a `playing` label.
- **equalizer** uses tiny changing level bars with no extra label.
- **spinner** uses a minimal orbit glyph next to `playing`.
- **jingle** holds one fixed note while a single sparkle slowly drifts away.
