package internal

import (
	"fmt"
	"slices"
	"strings"
)

// kindAliases abbreviate the command of a kind by the initials of its words, as long as these
// stay unambiguous among the kinds the API docs list: bbrun rather than bbr, which could as well
// be meshBuildingBlockRunner.
var kindAliases = map[string][]string{
	"buildingblock":                  {"bb"},
	"buildingblockdefinition":        {"bbd"},
	"buildingblockdefinitionversion": {"bbdv"},
	"buildingblockrun":               {"bbrun"},
	"eventlog":                       {"elog"},
	"workspace":                      {"ws"},
}

// KindAliases are the aliases of the command of a kind, which meshstack api-docs --describe takes too.
func KindAliases(command string) []string {
	return kindAliases[command]
}

// KindCommand names the command of a meshObject kind: meshBuildingBlock is buildingblock. It takes
// the command's name or one of its aliases as well.
func KindCommand(name string) string {
	command := strings.TrimPrefix(strings.ToLower(name), "mesh")
	for kind, aliases := range kindAliases {
		if slices.Contains(aliases, command) {
			return kind
		}
	}
	return command
}

// KindLong is the help of the command of a kind. It leaves what the kind is to the API docs, and
// so does every command of the kind.
func KindLong(short, command string) string {
	return fmt.Sprintf(`%s.

The meshStack API docs say what each operation on them takes and returns:

  meshstack api-docs --describe %s

meshstack api sends an operation that no command here covers.`, short, command)
}
