package tty

import (
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var NoInput = setting.Setting[bool]{
	Env:   "MESHSTACK_NO_INPUT",
	Short: "Never wait for a person. Also read from MESHSTACK_NO_INPUT.",
	Long: "Never wait for a person, also read from `MESHSTACK_NO_INPUT`.\n\n" +
		"It covers more than a prompt: a browser login fails at once instead of waiting ten minutes " +
		"for a callback nobody will complete.\n\n" +
		"It is the only thing that says nobody is coming. A pipe does not: stderr reaches a person " +
		"from one just as well as from a terminal.",
	Parse: setting.ParseBool,
}
