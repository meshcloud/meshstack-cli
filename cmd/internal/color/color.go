// Package color decides how much color the CLI writes, so that the Markdown a command shows and
// the log agree. A decision never makes the CLI interactive: a prompt asks a terminal UI only where
// cmd/internal/prompt finds a terminal.
//
// colorprofile, which lipgloss and charm.land/log use, reads NO_COLOR, CLICOLOR and
// CLICOLOR_FORCE only where they parse as a bool, and lets CLICOLOR_FORCE outrank NO_COLOR where
// the output is no terminal. It reads neither FORCE_COLOR nor a CI, which is why this package
// does.
package color

import (
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// Output is the color profile of what a command writes to w. Without a terminal, only a forcing
// variable turns color on, since a script that captures or pipes the output wants it plain.
func Output(w io.Writer, environ []string) colorprofile.Profile {
	env := parse(environ)
	if env.disablesColor() {
		// colorprofile would otherwise force ANSI for a CLICOLOR_FORCE that NO_COLOR overrides.
		withoutForce := slices.DeleteFunc(slices.Clone(environ), func(variable string) bool {
			return strings.HasPrefix(variable, "CLICOLOR_FORCE=")
		})
		// ASCII keeps bold and italic: NO_COLOR is about color, not about emphasis
		// (https://no-color.org).
		return min(colorprofile.Detect(w, withoutForce), colorprofile.ASCII)
	}
	return max(colorprofile.Detect(w, environ), env.forcedProfile())
}

// Log is [Output] for the log on w, which also takes color in a CI whose job log renders ANSI.
// The log is no output a script parses, so a CI colors it without being asked.
func Log(w io.Writer, environ []string) colorprofile.Profile {
	env := parse(environ)
	profile := Output(w, environ)
	if env.disablesColor() {
		return profile
	}
	return max(profile, env.ciProfile())
}

type env map[string]string

func parse(environ []string) env {
	e := env{}
	for _, variable := range environ {
		name, value, _ := strings.Cut(variable, "=")
		e[name] = value
	}
	return e
}

// disablesColor reads NO_COLOR as https://no-color.org defines it, where any value but the empty
// one counts, and FORCE_COLOR=0 or false as Node.js and chalk do.
func (e env) disablesColor() bool {
	return e["NO_COLOR"] != "" || e.isOff("FORCE_COLOR")
}

// forcedProfile reads FORCE_COLOR as Node.js and chalk do, where 2 asks for 256 colors and 3 for
// true color, and CLICOLOR_FORCE as https://bixense.com/clicolors defines it. Both count as unset
// while empty, as NO_COLOR does, so that a CI variable left empty forces nothing.
func (e env) forcedProfile() colorprofile.Profile {
	switch force := e["FORCE_COLOR"]; {
	case force == "2":
		return colorprofile.ANSI256
	case force == "3":
		return colorprofile.TrueColor
	case force != "" && !e.isOff("FORCE_COLOR"):
		return colorprofile.ANSI
	}
	if e["CLICOLOR_FORCE"] != "" && !e.isOff("CLICOLOR_FORCE") {
		return colorprofile.ANSI
	}
	return colorprofile.NoTTY
}

func (e env) isOff(name string) bool {
	return e[name] == "0" || strings.EqualFold(e[name], "false")
}

// ciProfile follows supports-color (https://github.com/chalk/supports-color), which chalk and
// most Node.js CLIs use: it names the CIs whose job log renders ANSI. CI=true alone is no such
// evidence, and a log viewer outside the list, such as Jenkins without its AnsiColor plugin, shows
// the escape codes as text. So does the system log of a building block run in meshPanel. Its
// runner starts a script or tofu with neither CI nor TERM set, which keeps a run plain: see
// cleanSystemEnv in tf-block-runner/tfrun of ../building-block-runner.
func (e env) ciProfile() colorprofile.Profile {
	has := func(name string) bool { _, found := e[name]; return found }
	switch {
	case has("GITHUB_ACTIONS"), has("GITEA_ACTIONS"), has("CIRCLECI"):
		return colorprofile.TrueColor
	case has("GITLAB_CI"), has("BUILDKITE"), has("TRAVIS"), has("DRONE"), has("APPVEYOR"),
		has("TF_BUILD") && has("AGENT_NAME"):
		return colorprofile.ANSI
	}
	return colorprofile.NoTTY
}
