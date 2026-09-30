# Demo

`demo.tape` records [`../demo.gif`](../demo.gif) with [VHS](https://github.com/charmbracelet/vhs),
which the `nix develop` shell provides. Re-record it from the repository root, on Linux:

```shell
task docs:demo
```

No meshStack and no network is needed: [`setup.sh`](setup.sh) builds the CLI and
[`fakemeshstack`](fakemeshstack), which serves three meshStack installations under `example.com`
through a local HTTPS proxy and plays the browser as a fake `xdg-open`. The CLI's configuration
goes into a temp directory, never into your own.
