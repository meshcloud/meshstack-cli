# Sourced by the tapes from the repository root. Everything, the CLI's config dir included, goes
# into a fresh temp dir, and the fake xdg-open has to come first on PATH, or no demo is recorded.
# The temp dir is directly in /tmp because the profile form shows the path of a stored credential,
# and a longer path takes more lines than the gif has.
demo_setup() {
	demo_dir=$(mktemp -d /tmp/meshstack-demo.XXXXXX) || return
	# A go.work above this checkout would not list the demo module.
	GOWORK=off go build -C docs/demo/fakemeshstack -o "$demo_dir/bin/fakemeshstack" . || return
	ln -s fakemeshstack "$demo_dir/bin/xdg-open" || return
	go build -o "$demo_dir/bin/meshstack" \
		-ldflags "-X github.com/meshcloud/meshstack-cli/cmd/internal.Version=$(git describe --tags --abbrev=0)" \
		./cmd/meshstack || return
	"$demo_dir/bin/fakemeshstack" serve "$demo_dir" 2>"$demo_dir/server.log" &
	demo_server=$!
	for _ in $(seq 50); do [ -f "$demo_dir/env" ] && break; sleep 0.1; done
	. "$demo_dir/env" || return
	export PATH="$demo_dir/bin:$PATH"
	[ "$(command -v xdg-open)" = "$demo_dir/bin/xdg-open" ] && [ "$(command -v meshstack)" = "$demo_dir/bin/meshstack" ]
}
demo_setup || exit 1
# VHS starts bash with +o history, and demo.tape recalls a command line.
set -o history
# VHS's own prompt. The \[ \] tell readline that the colour takes no columns, or a recalled command
# line is drawn off by their width.
PS1=$'\\[\e[38;2;90;86;224m\\]> \\[\e[0m\\]'
