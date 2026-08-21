# Media parts prototype

This branch prototype is tracked by missis ticket `#92`.

It adds three explicit prototype value kinds:

- `image` — a URI or image descriptor;
- `video` — a URI or video descriptor, optionally with a poster URI;
- `embed` — a URI or inline embed descriptor.

The CLI can write URI values today:

```text
missis set '#1/screenshot' https://example.test/screenshot.png --kind image
missis set '#1/recording' artifact:sha256:demo-video --kind video
missis set '#1/player' https://example.test/player --kind embed
```

SDK-level event construction may put `model.MediaDescriptor` in
`model.Value.Data`. The descriptor keeps the kind explicit and may carry
`uri`, `media_type`, `alt`, and `poster_uri`.

The TUI renders deterministic text cards. It does not fetch media, decode
binary content, emit terminal image protocol escapes, launch a player, or
execute HTML. Inline iframe input is reduced to a source notice and marked
`not executed`.

This is intentionally a compatibility-first prototype. Actual binary storage,
content-addressed artifact lifecycle, terminal capability detection, external
open/play actions, and web-view rendering remain separate follow-up work.
