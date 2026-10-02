package internal

import (
	"fmt"
	"slices"
	"strings"
)

// kindAliases abbreviate the command of a kind by the initials of its words, as long as these
// stay unambiguous among the kinds the API docs list: bbrun rather than bbr, which could as well
// be meshBuildingBlockRunner.
var kindAliases = map[KindCommand][]string{
	"buildingblock":                  {"bb"},
	"buildingblockdefinition":        {"bbd"},
	"buildingblockdefinitionversion": {"bbdv"},
	"buildingblockrun":               {"bbrun"},
	"eventlog":                       {"elog"},
	"workspace":                      {"ws"},
}

type KindCommand string

func KindCommandOf(name string) KindCommand {
	command := strings.TrimPrefix(strings.ToLower(name), "mesh")
	for kind, aliases := range kindAliases {
		if slices.Contains(aliases, command) {
			return kind
		}
	}
	return KindCommand(command)
}

func (command KindCommand) Aliases() []string {
	return kindAliases[command]
}

func (command KindCommand) Long(short string) string {
	return fmt.Sprintf(`%s.

The meshStack API docs say what each operation on them takes and returns:

  meshstack api-docs --describe %s

meshstack api sends an operation that no command here covers.`, short, command)
}
