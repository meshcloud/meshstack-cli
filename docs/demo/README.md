# Demo

Two tapes record the gifs with [VHS](https://github.com/charmbracelet/vhs), which the
`nix develop` shell provides:

- `demo.tape` records [`../demo.gif`](../demo.gif), the intro at the top of the README.
- `profile.tape` records [`../profile.gif`](../profile.gif) for [`../profile.md`](../profile.md).
  It starts with no profile.

Re-record both from the repository root, on Linux:

```shell
task docs:demo
```

No meshStack and no network is needed: [`setup.sh`](setup.sh) builds the CLI and
[`fakemeshstack`](fakemeshstack), which serves three meshStack installations and a cut-down copy
of the API docs under `example.com` through a local HTTPS proxy and plays the browser as a fake
`xdg-open`. Each installation is an
[`internal/testutil/fakemeshstack`](../../internal/testutil/fakemeshstack) server. The CLI's
configuration goes into a temp directory, never into your own.
