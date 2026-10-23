---
name: meshstack-cli
description: >
  Work with meshStack from the shell with the `meshstack` CLI. Covers login and profiles, how an
  agent logs in without a terminal, the `-o ndjson | jq` pipeline, the meshObject shape
  (metadata/spec/status) that makes filtering cheap, and `meshstack api` / `api-docs` for
  everything the other commands do not cover. Use when asked to list, inspect or debug workspaces,
  building blocks, building block definitions or building block runs, to report on the health of
  the building blocks a platform team provides, to trigger or wait for a run, or to order a
  building block.
---

# meshStack CLI

This skill is for agents and people who **use** the CLI. It lives in the CLI repository, so it
changes together with the commands. Repositories that use the CLI link here, and only add their own
facts (endpoint, profile name, workspaces).

`meshstack --help` and `meshstack <command> --help` are the source of truth for flags. When this
skill and the help disagree, trust the help.

| Command | Alias | Does |
|---|---|---|
| `meshstack workspace list` | | workspaces this login can see |
| `meshstack buildingblock list` | `bb` | building blocks (deployed instances), newest first |
| `meshstack buildingblock trigger-run <uuid>` | `bb` | starts a run of one block |
| `meshstack buildingblock tfstate` | `bb` | reads and writes the OpenTofu state of one block |
| `meshstack buildingblockdefinition list` | `bbd` | definitions — what platform teams offer |
| `meshstack buildingblockdefinitionversion list --definition <uuid>` | `bbdv` | the versions of one definition |
| `meshstack buildingblockrun list [--building-block <uuid>]` | `bbrun` | runs, newest first |
| `meshstack buildingblockrun logs <run-uuid>` | `bbrun` | the steps of one run, with their output |
| `meshstack eventlog list` | | event logs, newest first |
| `meshstack api <path> [-X method] [--request-json file\|-]` | | any meshStack API call, with this login |
| `meshstack api-docs [<path>] [--describe <kind>]` | | what the API docs say about a request |

Some commands are marked **experimental** in their help. Their output shape may still change, so
re-check a field before you trust a recipe below.

Creating or changing meshStack configuration goes through the
[meshStack Terraform provider](https://github.com/meshcloud/terraform-provider-meshstack), not this
CLI. The provider uses the same login: a `provider "meshstack" {}` block takes the endpoint and the
credential of the profile of the last login, and `profile` in the block picks another. A browser login
also needs `workspace` in the block or `MESHSTACK_WORKSPACE`, as the provider ignores the profile's
default workspace; an API key or an API token needs none. For several workspaces in one configuration
with a browser login, use one aliased provider block per workspace. The exception
is a one-off order that Terraform cannot converge on,
such as a starterkit: see [Ordering a building block](#ordering-a-building-block).

## Login and profiles

A login stores a credential in a profile. A profile also holds an endpoint and a default workspace.
`--endpoint`, `--workspace` and `--profile` are global flags, with `MESHSTACK_ENDPOINT`,
`MESHSTACK_WORKSPACE` and `MESHSTACK_PROFILE` as env equivalents.

`MESHSTACK_PROFILE`, `MESHSTACK_WORKSPACE` and a credential in the environment (`MESHSTACK_API_KEY`
with `MESHSTACK_API_SECRET`, or `MESHSTACK_API_TOKEN`) win over what a login stores, and a
command warns when they do. Unset them to use the stored profile. To ignore the stored profiles
instead, as a script or CI job should, point `MESHSTACK_CONFIG_DIR` at a directory of its own.

```sh
meshstack auth status -p <profile>       # who, which workspace, until when — check this first
meshstack login -p <profile> --endpoint <url> --workspace <id>   # browser login, needs a human
meshstack login --apikey=<id> --stdin    # API key, secret from stdin; the = is required
meshstack login --apitoken --stdin       # an access token, sent as it is
meshstack auth logout                    # drop this profile's credential
meshstack profile list                   # stored profiles and their endpoints
```

Work as the user, through their own browser login. Do not log in with a shared API key, for
example a foundation's: everything you do then shows up as the key, not as the person who asked.

### Logging in as an agent

A browser login needs the user in their browser. An agent's shell is no terminal, and two things
break the login there:

- **Always pass `-p` and `--workspace`.** Without `-p`, login asks which profile to log in to.
  Without `--workspace`, it asks for the workspace *after* the browser step: the prompt reads end
  of input and fails, and the finished browser login is lost
  ([#43](https://github.com/meshcloud/meshstack-cli/issues/43)). This also happens when the profile
  has a default workspace.
- **Run login in the background.** It waits up to 5 minutes for the browser step. Tell the user to
  finish it, and wait for the command to end. Never start a second login while one is waiting:
  the user then has to sign in twice.

```sh
meshstack login -p <profile> --endpoint <url> --workspace <id> </dev/null
```

A browser login lasts about a day. Within it, the CLI mints a token for any other workspace without
a new browser step, so log in once and switch workspaces with `--workspace`.

### Which workspace a command acts in

A browser login mints its token **per workspace**: `--workspace`, else `MESHSTACK_WORKSPACE`, else
the profile's default workspace. `meshstack auth status` shows which one. `--workspace` takes the
workspace identifier as meshPanel shows it (`metadata.name`, for example
`devops-platform`), not the display name.

- An object in another workspace answers `403`, and its runs come back empty. Set
  `MESHSTACK_WORKSPACE` once per shell rather than on each call.
- A login works only in a workspace where it holds a role, **also for an Organization Admin**:

  ```
  Error: this login has no role in workspace 'x'. A token for a workspace takes a role in it, also for an Organization Admin ...
  ```

  An admin can still read across workspaces from a session in the admin workspace.
- A wrong identifier errors too, so a missing workspace means a wrong identifier or a missing role,
  not an empty result.
- On `bbd list`, `--workspace` means *owned by* that workspace.

## Always pipe ndjson through jq

Commands for people (`auth status`, `profile list`) write Markdown. The others write one JSON array
by default. For anything you consume yourself, use `-o ndjson`: one JSON object per line, so `jq`
filters stream. A listing stops at 100 items unless you pass `--limit <n>` or `--limit unlimited`.

```sh
meshstack bb list --limit unlimited -o ndjson | jq -r '...'
```

**The payload is much bigger than it looks.** A single building block carries every input, every
output and often a `summary` output with a full markdown report; a hundred of them run to ~90 KB. A
`bbd list` is far worse: every definition embeds its icon as a base64 `spec.symbol`, which is most
of the response. Never pipe a raw list into your context. Project the two or three fields you need
in the same command that fetches them — and project with `jq`, not with `head`, which truncates
mid-object and makes the filter fail silently.

## The meshObject shape

Every object the CLI prints has the same three top-level keys:

```
metadata   identity — thinner than you expect, see below
spec       desired state — what someone asked for
status     observed state — what meshStack made of it
```

Field names worth knowing:

- **Workspace** — `metadata.name` is the identifier used everywhere else, `spec.displayName` is
  prose. `metadata` also carries `createdOn`, `deletedOn` and `tags`.
- **Building block** — `metadata` carries **only** `uuid` and `ownedByWorkspace`. Everything else
  lives in `spec.displayName`, `spec.buildingBlockDefinitionVersionRef.uuid`, `spec.targetRef.kind`
  (`meshWorkspace` or `meshTenant`), `status.status`, `status.lifecycle.state`,
  `status.latestRunUuid`, `status.latestDryRunUuid`, `status.forcePurge`.
- **Definition** — `metadata.uuid`, `metadata.ownedByWorkspace`, `spec.displayName`,
  `spec.targetType` (`TENANT_LEVEL` or `WORKSPACE_LEVEL`), `spec.supportedPlatforms[]`,
  `spec.approvalPolicies`, `spec.schedule`, and the huge `spec.symbol`. `status.versions[]` maps
  `versionUuid` to `versionNumber` and `state`; `status.latestReleasedVersionUuid` names the
  current one.
- **Definition version** — `metadata.uuid` is the uuid a building block's
  `spec.buildingBlockDefinitionVersionRef` points at. `spec.versionNumber`, `spec.state`,
  `spec.deletionMode`, `spec.dependencyRefs`, `spec.implementation` (for example `terraform` with
  `repositoryUrl`, `repositoryPath`, `refName`), and `spec.inputs` / `spec.outputs` as maps of name
  to a declaration (`displayName`, `type`, `assignmentType`, `isSensitive`, `argument` for a
  statically bound value). `status.usageCount` counts the blocks on that version.
- **Run** — `metadata.uuid`, `spec.runNumber`, `spec.behavior` (`APPLY`, `DETECT`), `status` (a
  plain string here, not an object). `spec.buildingBlock` repeats the block's placement, but its
  `projectIdentifier` and `fullPlatformIdentifier` are usually null — prefer the block's own
  `metadata.ownedByWorkspace`.
- **Run logs** — not a meshObject and not one line per item: a single object with a `steps[]` array
  of `{displayName, status, userMessage, systemMessage}`. `systemMessage` is the raw runner output,
  `userMessage` the condensed message shown to the consumer.

Traps worth knowing before you read any of it:

**`status.lifecycle.state` decides whether a failure matters.** `ACTIVE` means someone still depends
on the block. `MARKED_FOR_DELETION` means it is on its way out, and a failure there is usually a
destroy that did not finish — noise in a health report, not an incident. Always project this field
next to `status.status`, or you will report several times more problems than exist.

**The list commands print only `state` of the lifecycle.** The full `status.lifecycle` — timestamps
for `created`, `markedForDeletion` and `deleted`, each with an `author` — lives on newer API
versions. A run's `metadata.createdOn` also comes back empty. So when the question is "when" or
"who", read the object through `meshstack api` (see
[Everything else: `meshstack api`](#everything-else-meshstack-api)) rather than guessing from
`spec.runNumber`.

**`spec.displayName` on a building block is user-chosen, not the definition name.** Consumers rename
their instances, so one definition version turns up under many unrelated names. To group blocks by
*what the platform team provides*, key on `spec.buildingBlockDefinitionVersionRef.uuid` and treat
displayName as a label. `bbd list` resolves that uuid to a real name — see the recipe below.

`spec.inputs` and `status.outputs` on a building block are maps of name to
`{value, valueType, assignmentType, isSensitive}`. So `spec.inputs.project_name.value` is the value;
`spec.inputs.project_name` alone is four times the tokens.

## Recipes

One line per building block — start here, then drill into a single uuid:

```sh
meshstack bb list --limit unlimited -o ndjson |
  jq -r '[.metadata.uuid, .metadata.ownedByWorkspace, .spec.displayName,
          .status.status, .status.lifecycle.state] | @tsv'
```

Failures that actually need attention — anything still `ACTIVE`:

```sh
meshstack bb list --limit unlimited -o ndjson |
  jq -r 'select(.status.status != "SUCCEEDED" and .status.lifecycle.state == "ACTIVE") |
         [.metadata.uuid, .metadata.ownedByWorkspace, .spec.displayName,
          .status.latestRunUuid] | @tsv'
```

The definitions on offer, one line each — drop `spec.symbol` or you pull the base64 icons:

```sh
meshstack bbd list --limit unlimited -o ndjson |
  jq -r '[.metadata.uuid, .metadata.ownedByWorkspace, .spec.displayName,
          .spec.targetType, (.status.versions|map("v\(.versionNumber) \(.state)")|join(","))] | @tsv'
```

Fleet health per definition version, with readable names. `bbd list` carries the whole
version-uuid-to-name map, so build it once and join the block list against it:

```sh
meshstack bbd list --limit unlimited -o ndjson |
  jq -r '.spec.displayName as $n | .status.versions[] |
         [.versionUuid, "\($n) v\(.versionNumber)"] | @tsv' | sort > /tmp/bbdv-names.tsv

meshstack bb list --limit unlimited -o ndjson |
  jq -r '[.spec.buildingBlockDefinitionVersionRef.uuid, .status.status] | @tsv' | sort |
  join -t $'\t' - /tmp/bbdv-names.tsv | cut -f2,3 | sort | uniq -c | sort -rn
```

Which module and Git ref a definition's versions are pinned to — the fastest way to see whether a
deployed definition lags the `ref=` in your IaC:

```sh
meshstack bbdv list --definition <definition-uuid> -o ndjson |
  jq -r '.spec.implementation.terraform as $t |
         [.spec.versionNumber, .spec.state, (.status.usageCount|tostring),
          $t.repositoryPath, $t.refName] | @tsv' | sort -n
```

`refName` is empty on older versions that were not pinned to a commit.

What a definition version declares as inputs, and how each one is assigned:

```sh
meshstack bbdv list --definition <definition-uuid> -o ndjson |
  jq -r 'select(.spec.versionNumber == <n>) | .spec.inputs | to_entries[] |
         "\(.key)\t\(.value.type)\t\(.value.assignmentType)"'
```

Inputs of a deployed block as flat `name=value` pairs, sensitive ones masked:

```sh
meshstack bb list --limit unlimited -o ndjson |
  jq -r 'select(.metadata.uuid == "<uuid>") | .spec.inputs |
         to_entries[] | "\(.key)=\(if .value.isSensitive then "<sensitive>" else .value.value end)"'
```

Outputs without the markdown report:

```sh
meshstack bb list --limit unlimited -o ndjson |
  jq -r 'select(.metadata.uuid == "<uuid>") | .status.outputs |
         del(.summary) | to_entries[] | "\(.key)=\(.value.value)"'
```

The `summary` output is a rendered markdown report. Read it on its own, and only when you need the
human-facing story:

```sh
meshstack bb list --limit unlimited -o ndjson |
  jq -r 'select(.metadata.uuid == "<uuid>") | .status.outputs.summary.value'
```

Run history for one block:

```sh
meshstack bbrun list --building-block <uuid> -o ndjson |
  jq -r '[.metadata.uuid, (.spec.runNumber|tostring), .spec.behavior, .status] | @tsv'
```

Why a run failed — only the steps that broke, and their consumer-facing message:

```sh
meshstack bbrun logs <run-uuid> -o ndjson |
  jq -r '.steps[] | select(.status != "SUCCEEDED") |
         "== \(.displayName) [\(.status)]\n\(.userMessage // .systemMessage)"'
```

`systemMessage` on a successful step holds the full `tofu init`/`apply` transcript and runs to tens
of kilobytes. Read one step at a time when you need it:

```sh
meshstack bbrun logs <run-uuid> -o ndjson |
  jq -r '.steps[] | select(.displayName == "Run Terraform Apply") | .systemMessage'
```

## Triggering a run

`trigger-run` asks meshStack to run a block again, for example after a fix was pushed to the branch
its definition points at. meshStack runs the block itself, so no cloud credential is needed here.

```sh
meshstack bb trigger-run <uuid> -o ndjson | jq -r '.status.latestRunUuid'
```

It waits until meshStack has started the run, and prints the block; `status.latestRunUuid` names
the new run. It never approves: when the definition gates manual triggers
(`spec.approvalPolicies.manualTriggers`), the run waits until a person approves it in meshPanel.
Every call starts a run, so trigger once and then poll.

## Approval gates and preflight runs

A definition with `approvalPolicies` does not apply a gated change straight away. meshStack first
runs a **preflight**: a dry run, listed with `spec.behavior == "DETECT"`. The `APPLY` run after it
parks in `WAITING_FOR_APPROVAL` until someone approves it in meshPanel. The approver sees the
preflight's plan, so read it yourself before you ask for the approval:

```sh
meshstack bbrun logs <preflight-run-uuid> -o ndjson |
  jq -r '.steps[].systemMessage // ""' |
  grep -E "will be (created|destroyed|updated|replaced)|must be replaced|Plan:"
```

Report the `Plan:` line and anything unexpected, and hand the user a deep link (below). A
Terraform apply that updates such a block ends with the provider warning `Building block run is
waiting for input or approval` — that is the gate, not an error.

## Linking into meshPanel

Give the user a link whenever you ask them to act in meshPanel. A building block, by uuid:

```
https://<meshpanel-host>/#/s/<workspace>/building-blocks/building-blocks-overview?healthStatus=NONE,SUCCEEDED,FAILED&buildingBlock=<uuid>
```

meshPanel's own links carry a numeric id of meshStack's internal API instead of the uuid. That id is
not exposed through the public API, so do not try to build those.

## Waiting for a run

Poll the block's status until it reaches a terminal state, and print only on change. Every terminal
state has to end the loop, or a failure looks like a run that is still going:

```sh
B=<uuid>; prev=""
while true; do
  s=$(meshstack bb list --workspace <workspace> --limit unlimited -o ndjson 2>/dev/null |
      jq -r "select(.metadata.uuid==\"$B\") | .status.status" || true)
  [ -n "$s" ] && [ "$s" != "$prev" ] && echo "block status: $s" && prev=$s
  case "$s" in SUCCEEDED|FAILED|ABORTED) exit 0;; esac
  sleep 30
done
```

A block that deletes itself at the end of its run, like a starterkit, drops out of `bb list`. Poll
its runs instead: `meshstack bbrun list --building-block $B --workspace <workspace>` until the
`APPLY` run is terminal.

## Everything else: `meshstack api`

`meshstack api` sends an authenticated, `curl`-like request to any path of the API, with the
current login. For a meshObject endpoint it picks the latest API version and sets the `Accept` and
`Content-Type` headers itself; `--api-version` picks another one. An answer outside 2xx still
prints its body, but the command fails.

Do not read the OpenAPI document yourself — it is several MB. Ask `api-docs`:

```sh
meshstack api-docs --describe bb               # every operation of a kind, latest version
meshstack api-docs --describe bb.list          # one operation, with its parameters
meshstack api-docs -X POST /api/meshobjects/meshbuildingblocks   # the same line as the api call
```

Read one object in another version, for example the full lifecycle of a building block:

```sh
meshstack api --api-version v2-preview /api/meshobjects/meshbuildingblocks/<uuid> |
  jq '.status.lifecycle'
```

Check with `api-docs` which version a kind offers first: some versions cover only part of a kind,
for example only tenant-level building blocks.

## Ordering a building block

Look up the definition version uuid and the `USER_INPUT` inputs with the `bbdv list` recipe above,
then post the order through `meshstack api`. A workspace-level block targets
`{kind: "meshWorkspace", name: <workspace>}`; a tenant-level one targets
`{kind: "meshTenant", uuid: <tenant-uuid>}`:

```sh
jq -n '{apiVersion: "v2-preview", kind: "meshBuildingBlock", spec: {
    displayName: "my-project",
    buildingBlockDefinitionVersionRef: {kind: "meshBuildingBlockDefinitionVersion", uuid: "<version-uuid>"},
    targetRef: {kind: "meshWorkspace", name: "<workspace>"},
    inputs: {name: {value: "my-project", valueType: "STRING"}}}}' |
  meshstack api -X POST /api/meshobjects/meshbuildingblocks --workspace <workspace> \
    --api-version v2-preview --request-json - |
  jq -c '{uuid: .metadata.uuid, status: .status.status}'
```

The order's author is the logged-in user, so a starterkit grants them its roles. A draft definition
version can only be ordered in the workspace that owns it.
