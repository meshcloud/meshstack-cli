---
name: meshobject-command
description: Use when adding or changing a `meshstack` command for a meshObject kind of the meshStack API (building blocks, runs, definitions, workspaces, event logs, ...), to name it and build it from the CLI's shared parts.
---

# A command for a meshObject kind

The tree's structure, one package per subcommand and one file per leaf, is the `command-tree`
rule of `CLAUDE.md`. Before you extend `client/`, follow the API-docs rule in its `client-package`
section.

## Name

A command is the meshObject kind, lowercased, without its `mesh` prefix:

| Kind | Command |
|---|---|
| meshBuildingBlock | `buildingblock` |
| meshBuildingBlockRun | `buildingblockrun` |
| meshBuildingBlockDefinition | `buildingblockdefinition` |
| meshBuildingBlockDefinitionVersion | `buildingblockdefinitionversion` |
| meshWorkspace | `workspace` |
| meshEventLog | `eventlog` |

Commands that are no meshObject are the exceptions: `auth` with its `login` and `logout`
shortcuts, `profile`, `api`.

The parent command declares its name as an `internal.KindCommand` and takes its `Aliases` from
it, and `meshstack api-docs --describe` takes them too. Add an alias to `kindAliases`, by the
initials of the kind's words, only while no other kind of the API docs has the same initials:
`bbrun`, since `bbr` could be meshBuildingBlockRunner as well.

## Help

The help says how to use a command, never what its kind is or does: the parent's `Long` is
`KindCommand.Long`, which points to `meshstack api-docs --describe <kind>`, and a leaf points to
`--describe <kind>.<action>` rather than explain the fields of an answer. Every leaf has an
`Example`, one line of it through the alias. A leaf checks what it can before it asks meshStack,
such as that a uuid parses, because meshStack answers a malformed one with an empty list or a 404.

## Leaf

- `list` lists the kind with `flags.Run[M](cmd, filter)` of `internal.ListFlags`, which pages through
  `client.Raw.List[M]`.
- `show` shows one object.
- A sub-resource or an action is named after its path element in the API: `buildingblockrun logs`
  for `…/logs`, `buildingblock trigger-run` for `…/trigger-run`.

## Output

A command writes JSON: a listing through `internal.ListFlags`, one object through
`OutputFlag.RegisterForItem` and `internal.WriteItem`, both json or ndjson. Only a command meant
for people, such as `auth status` or `profile show`, offers Markdown, through `internal.ShowFlag`
and a `cmd/internal/markdown` template next to the command.

## Reuse

- Flags come from `cmd/internal`: `internal.Flag` and `NewFlagForSetting` for one that is a
  setting, and the global `WorkspaceFlag` and `ProfileFlag`. `internal.ListWorkspace` gives the
  workspace a listing narrows to.
- A command gets its client from `internal.ResolveClient`, or for the session itself from
  `auth.ResolveSession(ctx, internal.ResolveClientOptions(...))` and `Session.Client`. A
  `ResolveClientOptionsModifier` such as `internal.SkipVersionCheck` adapts the resolution.
- What the CLI writes as meshStack sent it goes through `client.Raw`: `List[M]` with the kind's
  list filter struct, `Get[M]` for one object or a sub-resource below it, and `DoRequest` for any
  other path. `M` is the Go type of the kind's typed client, which `client.New` registers with
  `Raw`; register a kind there before a command reads it.
- A list filter struct holds every query parameter the API docs give its endpoint, a repeated one
  as a slice. A parameter the command always sends, such as `includeAllPublished`, is a field the
  command sets. Any other client method only the CLI calls goes on a new `client.Client` field, so
  the Terraform provider's mocks keep compiling.
- Prompts go through `cmd/internal/prompt`.
