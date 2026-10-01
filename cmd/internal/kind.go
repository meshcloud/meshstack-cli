package internal

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
