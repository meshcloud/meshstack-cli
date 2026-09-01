package tty

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

var NoInput = setting.Value[bool]{
	EnvKey: envKey,
	Short:  "Never wait for a person. Also read from MESHSTACK_NO_INPUT.",
	Long: "Never wait for a person, also read from `MESHSTACK_NO_INPUT`.\n\n" +
		"It covers more than a prompt: a browser login fails at once instead of waiting ten minutes " +
		"for a callback nobody will complete.\n\n" +
		"It is the only thing that says nobody is coming. A pipe does not: stderr reaches a person " +
		"from one just as well as from a terminal.",
	Parse: parseNoInput,
}

func parseNoInput(text string) (bool, error) {
	trimmed := strings.TrimSpace(text)
	value, err := strconv.ParseBool(trimmed)
	if err != nil {
		return false, fmt.Errorf("%q is neither true nor false; accepted values are true, false, 1 and 0", trimmed)
	}
	return value, nil
}
