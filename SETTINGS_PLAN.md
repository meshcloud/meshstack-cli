# Plan — resolving a session from settings

One way to declare a configuration item, one way to resolve it, shared by the meshStack CLI and the
Terraform provider. Each item lives in the domain package it belongs to, carries its own
`MESHSTACK_*` name and its own help, and reports where its value came from.

This is the specification of what is left to build. It states the design, not how the branch got
here, so a decision it records is a decision to hold rather than one to revisit. **If implementing a
step shows a decision is wrong, revisit the decision and edit this file** — there is no second
document that tracks where the plan and the code disagree.

## What is already there

- **`pkg/setting`** — the mechanism. `Value[T]{EnvKey, Short, Long, Parse}` with `Help()`, the
  `Source` interface (`Lookup(key) (string, bool)` and `Describe(key) string`), and
  `Resolve[T](v, sources...) (T, Source, error)`, which walks the sources it is handed and stops at
  the first non-empty answer. It knows nothing about which sources those are.
- **Eight declarations**, in the domain packages, with no `Setting` suffix: `meshstack.Endpoint`,
  `meshstack.Workspace`, `profile.Name`, `profile.ConfigDir`, `credential.ApiKeyId`,
  `credential.ApiSecret`, `credential.ApiToken`, `tty.NoInput`. `declarations_test.go` enumerates
  them and pins that each has a one-line `Short`, a non-nil `Parse` and a unique `MESHSTACK_*` key.
- **`pkg/credential`** — `Method` with `MethodLogin`, `MethodApiKey` and `MethodManual`; the
  `Credential` sum with `Current` as the selection and three pointers as the set; the three
  constructors; `Validate`; and each method's own token cache, so no cached token can be handed out
  by a method that did not mint it.
- **`pkg/meshstack`** — `ParseEndpoint`, `SameEndpoint`, `WorkspaceScope`, `Unscoped`, `ClaimKey`,
  `ErrMissing`.
- **`pkg/profile`** — `config.json` and `credentials/<profile>.json`, both `encoding/json/v2`, both
  written atomically and in a deterministic order. `ConfigDir` is the one path setting; the
  credentials directory is `<ConfigDir>/credentials` by convention.

Two settings are not settings and stay as they are. `MESHSTACK_NO_BROWSER` is a private const in
`pkg/oidc/browser`, the only package allowed to read it, and means something `MESHSTACK_NO_INPUT`
does not — "do not launch a browser, but still print the URL and wait, because a person is coming".
`MESHSTACK_SKIP_VERSION_CHECK` is read in `client/mesh_info.go`, and `.golangci.yml` forbids `client/`
to import `pkg/setting` at all.

## 1. Which package resolves what

Two packages resolve, and the split is by domain rather than by convenience.

**`pkg/profile` answers "which profile, and what does `config.json` say about it".** That needs no
credential and no token, so it does not belong in `pkg/auth`.

```go
// Selection is which profile this run uses, and what config.json says about it.
type Selection struct {
    Name     string
    Entry    Profile           // zero when Exists is false
    Exists   bool
    Endpoint string            // what a source above the profile named, empty when none did
    Origins  []setting.Origin
}

func Select(ctx context.Context, sources ...setting.Source) (Selection, error)
```

It takes a context because it emits one of the three warnings in 6 — a profile picked by matching
the endpoint — and it warns where the decision is made rather than reporting the fact upward for
`pkg/auth` to phrase. `meshstack profile set` calls `Select` directly, so a warning owned by
`pkg/auth` would not reach it.

**`Exists` is reported, not judged.** Today `selectProfile` refuses a named profile that is not in
`config.json`, and refuses an endpoint no profile matches, unless it was called for a login. That
exception is now derived rather than flagged: `ResolveSession` raises both errors when
`opts.Store` is nil, and tolerates both when it is not. Handing a store in says this command
writes the profile, and a profile a command is about to write need not exist yet.

`pkg/profile` also absorbs the four `config.json` operations that sit in `pkg/auth/profiles.go`
today, because all four only read and write that one file: `List`, `Ensure`, `SetEndpoint` and
`SetWorkspace`, plus `DefaultName` and `DescribeConfigPath`. `pkg/auth/profiles.go` disappears.
`List` returns `[]Summary`, because the struct and the function cannot both be `Known` and neither
name wants to be the adjective. `DescribeConfigPath` is exported only until `selectProfile`, its
three remaining callers, becomes `Select` here.
`pkg/profile` imports `pkg/meshstack` for `SameEndpoint` and `ParseEndpoint`. There is no cycle:
`pkg/meshstack` reaches only `pkg/oidc/scope`, `pkg/diags`, `client/types/xurl` and `pkg/setting`,
and `pkg/setting` reaches only `pkg/diags`.

**`pkg/auth` answers "who am I, against what, in which workspace".** It calls `profile.Select` and
owns everything downstream of it.

```go
type ResolveSessionOptions struct {
    Settings     setting.Source
    DemandMethod credential.Method
    Store        profile.Store   // nil: chosen from the credential's winning source, per 4
}

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (*Session, error)
```

An options struct rather than bare arguments, so a later addition — a clock, a second source — is a
new field instead of a signature change both front ends have to follow. A nil `Settings` is a front
end contributing nothing explicit, not an error.

`profile.ConfigDir` does not appear in either signature. Neither front end offers a config-directory
flag or block attribute, so its ranked list is the environment and the built-in default, and
`pkg/profile` resolves it where it opens its files. Threading a path through `LoadConfig`,
`NewFileStore` and `CredentialsPath` would buy nothing.

### `pkg/setting` gains three pieces of mechanism

`Origin` moves here from the sketch in `pkg/auth`, because both resolving packages now produce
origins and both need the type. It is mechanism: a key, and what the winning source said about it.

```go
type Origin struct {
    Key    string   // the setting's EnvKey, which is its identity
    Source string   // "--endpoint", "MESHSTACK_ENDPOINT", "profile dev", "built-in default"
}

func Environ() Source                          // Lookup is os.Getenv, Describe is the key
func Default(value string) Source              // the bottom of a ranked list
func DefaultFunc(f func() (string, bool)) Source
```

`Default` and `DefaultFunc` answer whatever key they are asked, because a default source is only
ever placed in one setting's list. `DefaultFunc` exists for a default that can fail — a directory
derived from `os.UserConfigDir` — where the failure is `("", false)` and the hand-written message
from 6 says what is missing.

## 2. The order, written out

```
explicit → environment → profile → built-in default
```

`ResolveSession` walks each setting's list in order and stops at the first hit, so a source below
the winner is never consulted. The list differs per setting, and it is named at the resolution
rather than on the declaration — a tier list on a `setting.Value` would teach `pkg/setting` the word
"profile".

| # | resolve | from |
|---|---|---|
| 1 | the profile selection, through `profile.Select` | `Endpoint` from explicit → environment; then `Name` from explicit → environment → the only profile matching that endpoint → `currentProfile` → `"default"` |
| 2 | `Endpoint`, second instalment | the selected profile, only when 1 found none |
| 3 | the credential, as one unit per 3 | explicit → environment → the profile's credentials file |
| 4 | `Workspace` | explicit → environment → the profile's `defaultWorkspace`; no default |
| 5 | `NoInput` | explicit → environment → `false` |

**Steps 1 and 2 are one list in two instalments, not two resolutions.** `Endpoint`'s list is
explicit → environment → profile throughout. Step 1 evaluates the prefix, because the profile is
what it is being used to pick; step 2 evaluates the one remaining source, and only when the prefix
came back empty. Re-reading the prefix could not change the answer, which is why step 2 is a single
lookup rather than a second walk.

`Endpoint` has no built-in default, so a missing endpoint after step 2 is an error.

**There is no interactive tier, because a prompt is not a source.** `ResolveSession` reports that it
found no value, and `cmd/auth/login.go` — the one command allowed to ask — prompts and resolves
again with the answer in its own explicit source. See 5 for how it recognises which failure it may
answer.

**The workspace prompt is why the prompt cannot be a source.** It offers a list fetched from
meshStack, built from an *unscoped* token: with no `c:` scope the principal holds only the list
rights, which is exactly what a user has before picking a workspace. So it needs a finished
`Session` — a resolved endpoint, a resolved credential and a network call — and it cannot run inside
the resolution that produces one. `Session.Workspaces` runs after `ResolveSession` returns.

**A missing workspace is not an error here.** Step 4 resolves and never demands.
`Session.RequireWorkspace` stays a post-resolution call, made by `provider.go` and by the commands
that act on meshObjects, and deliberately not by `meshstack workspace list` or `meshstack auth
status` — the two commands the error message itself tells the user to run. Folding the check into
`ResolveSession` would make the escape hatch the message names unreachable.

**Delete the `strings.TrimSpace` guards around the workspace.** They exist because
`workspace.Name.Empty()` trimmed and a plain `== ""` would not. Once every workspace reaches a call
site through step 4, it was parsed with `setting.Text`, which trims, so `== ""` is the same check
the method was.

## 3. A credential resolves as a unit

> **A credential resolves as a unit, in one walk down the ranked sources. The first source carrying
> an identity defines the credential. Its secret is its own secret slot when it has one; otherwise
> the first secret offered by a source that carries no identity of its own; otherwise nothing.**

**This walk is not `setting.Resolve`.** It reads `credential.ApiKeyId`, `ApiSecret` and `ApiToken`
out of each source by their `EnvKey`, but the pairing is its own code in `pkg/auth`, because
`setting.Resolve` resolves one value and this rule relates three.

| case | identity from | secret from | why |
|---|---|---|---|
| provider block `apikey`, `MESHSTACK_API_SECRET` | block | environment | the block has no `apisecret`, and the environment carries no id |
| `login --api-key=k`, secret prompted | flag | prompt | a flag never carries a secret, and nothing else offers one |
| profile holds `dev-key` + stored secret, `MESHSTACK_API_SECRET` exported | profile | the profile's own slot | its own slot wins before anything else is asked |
| block `apikey = "k"`, stale `MESHSTACK_API_KEY=j` + `MESHSTACK_API_SECRET=s` | block | nothing, so this fails | the environment carries a competing id, so its secret is skipped |

**Row 1 is the case to protect, not an edge case.** An id in the provider block, or on `--api-key`,
with the secret in the environment is the normal non-interactive setup. An acceptance test on both
front ends says so.

**The exclusion in row 4 fires against one source and no other.** Only a source offering an id of
its own is skipped, and that is the environment — the profile always stores its id beside its
secret, and a prompt carries neither. So the cost is one `Lookup` and one branch.

**Row 4 has to explain itself, or it is worse than the 401 it replaces.** The provider says
approximately:

> The provider block sets `apikey = "k"`, and no API secret is available for it.
> `MESHSTACK_API_SECRET` is set, but it belongs to `MESHSTACK_API_KEY` (`j`), which the block
> overrides. Set `apisecret` in the provider block, or unset `MESHSTACK_API_KEY` so that
> `MESHSTACK_API_SECRET` pairs with the block's `apikey`.

Quoting the losing id is deliberate: a client id is not a secret, and it is the fact that identifies
which stale export to remove. The CLI says the same with `--api-key` in place of the block. **Both
belong in the docs**, next to the row 1 setup they qualify.

### A token is an identity

`MESHSTACK_API_TOKEN` counts as an identity in the walk, and so does a profile's `Manual`. It
matters most for the exclusion clause: without it, a source holding a token would sit quietly and
still hand its secret to another source's `apikey`. **A token needs no secret**, so the second half
of the unit rule does not run for it.

**One source carrying both a token and a key id is an error**, not a precedence contest. They are
two different methods rather than two spellings of one thing, so choosing silently hands the user an
identity they did not pick. The message follows row 4's shape, naming both and saying to remove one.
Only the environment and the provider block can hold both — the CLI has no `--api-token` flag,
because a token is a secret and 5 keeps secrets out of flag values.

### A demanded method filters every source

`ResolveSessionOptions.DemandMethod` is not a setting: it has no `MESHSTACK_*` name and supplies no
value. It says which method the caller insists on, and only `meshstack auth login` and
`--dev-local` set it.

> **A demanded method filters what every source may offer.** A source that carries a different
> method's identity is passed over as if it were empty. With none demanded, the first identity wins,
> and a profile's `Current` decides which of its stored methods that is.

Without it a bare `meshstack login`, which demands `login`, would resolve an exported
`MESHSTACK_API_KEY` and refuse to open a browser. It is also what makes a bare `--api-key` reuse
the id already in the profile: the flag demands `apiKey` without supplying an id.

### Row 3 warns. It does not rewrite the profile.

Row 3 costs the rotation case: the user exports a rotated secret, the profile keeps serving the old
one, and meshStack answers 401 with nothing said. **The fix is a warning, not a write.**

Having the resolution store the environment's secret into the profile is rejected for three reasons.
**Which secret is newer is not knowable** — a stale `MESHSTACK_API_SECRET` in a shell beside a
profile logged in an hour ago is at least as common, and overwriting there destroys a working
credential that unsetting the variable does not bring back. **It makes a read path write**, so every
command contends for a lock that exists for a rare grant, including each resource in a
`terraform plan`. And **failing when the update was not possible** turns a cache-write failure into a
command failure, where a read-only home directory in a container is ordinary today.

So: keep the profile's paired secret, and warn when the environment carries a different one.

> `MESHSTACK_API_SECRET` is set and differs from the secret stored for `dev-key` in profile `dev`.
> The stored one is being used. To replace it, run
> `meshstack login --api-key=dev-key --api-secret-stdin`.

Skip the comparison when the profile stores a `SecretCommand` rather than a literal, because
comparing would mean running that command during a resolution.

This contradicts `token.go:335-343` and its comment, which puts the environment above the profile
for the secret. That is a bug: with `MESHSTACK_API_SECRET` exported and a profile holding a
different key, the call becomes `apiLogin(dev-key, ci-secret)` and meshStack answers 401. The
`!stored` half of that condition is right and survives; the `env(envApiSecret) != ""` half goes.

## 4. The store

`Session` mints and caches through exactly one store, because `degradeToMemory` replaces it and a
second one would silently stop being replaced.

**A command that configures a profile hands its store in.** `profile.NewFileStore(name)` is
`pkg/profile`'s entry point, and `cmd/auth/login.go`, `cmd/auth/logout.go` and `--dev-local` call it
themselves after `profile.Select`. That is what makes `meshstack login --api-key=k` write the secret
to disk while an ordinary command with the same environment does not.

**Otherwise `ResolveSession` chooses, and the rule is one line:** the store is the profile's
credentials file when the credential came from it, and a memory store when it came from anywhere
above. A CI job and a building block run therefore need no files at all, and an ephemeral secret
from HCL, the environment or a prompt lands in a store that cannot write. **That guarantee is
structural and load-bearing.**

`Session.whole` disappears rather than being replaced. It exists for two jobs and neither survives:
choosing the store, which is the rule above, and telling `mintManual` to re-read the token from the
front end, which eager resolution has already done.

**The profile is two sources, not one, and each opens its file on first use.** A config source over
`config.json` answers step 1; a credentials source over `credentials/<profile>.json` is consulted
only in step 3. Without the split, a run whose credential came wholly from the environment would
open a credentials file it never reads — and `resolve.go:183`'s "this credential belongs to a
different meshStack" check sits behind exactly that short-circuit, so a machine whose default
profile points at another meshStack would start failing for no reason.

## 5. Secrets are never a flag value

Two new flags, `--api-secret-stdin` and `--api-token-stdin`, each reading exactly one line.

Docker, helm, `az acr`, crane and gh all gate stdin behind a flag, and one reason settles it here:
**with stdin implicit, "was a secret piped?" cannot be answered.** `!IsTerminal(stdin)` is true in
every CI job, every container without `-t` and every `< /dev/null`. The only way to tell is to read,
which consumes a line that may belong to the command and blocks forever on an open pipe nobody
writes to. The risk this removes is not a script that pipes a secret on purpose; it is one that
hands the CLI a non-terminal stdin by accident — the provider's `scratch/headless-login.sh` runs it
with `</dev/null`, where a `--api-key` today reads an empty secret and gets a silent 401.

Behind a flag it is an explicit source, so it outranks the environment by the ranking in 2 with no
special case. When `MESHSTACK_API_SECRET` is also exported, one `slog.WarnContext` names it as
ignored — not a hard error, because `gh auth login --with-token` exits 1 when `GH_TOKEN` is set and
that breaks exactly the CI runs which export the variable on purpose.

`cmd/` reads stdin once, **before** `ResolveSession`, and contributes the result as a plain string in
the CLI's own source. A `Source` has neither a context nor an error return, so a read that blocks
would have nowhere to report itself. `readLine` in `internal/cli/secret.go` survives unchanged,
including its reason for reading exactly one line. A stored `SecretCommand` stays lazy behind
`credential.ApiKey.Resolve(ctx)`.

`internal/cli/secret.go`'s `readSecret` goes, and with it the order its doc comment defends. What
survives is `readLine`, `promptSecret` and `echoOff`, which now serve `meshstack login` alone.

### The three failures a login may answer with a prompt

`meshstack login` prompts and resolves again. To know which failure it may answer, `pkg/auth`
exports three sentinels, and `errors.Is` is how the command recognises them:

```go
var (
    ErrNoEndpoint     = errors.New("no meshStack endpoint")
    ErrNoApiSecret    = errors.New("no API key secret")
    ErrNoApiToken     = errors.New("no meshStack API token")
)
```

Every other command reports the error as it came. That is the cost 2 accepts:
`MESHSTACK_API_KEY=k` exported, no secret anywhere, at a terminal, running
`meshstack buildingblock list` now errors instead of prompting, naming
`meshstack login --api-key=k`. A command that is not `login` should not open a login dialogue.

The sentinels also delete `askForEndpoint`'s two guesses. It no longer infers which failure just
happened, and it no longer keeps its own copy of profile selection at `cmd/auth/login.go:200-206` —
which is stale anyway, since it omits `MESHSTACK_PROFILE` and the endpoint match, so
`MESHSTACK_PROFILE=staging meshstack login` prompts about the wrong profile. It calls
`profile.Select` like everything else.

## 6. Origins, help and warnings

`pkg/auth/resolve.go:41`'s `sources map[string]string` goes. It is assembled at eight call sites and
is sometimes wrong: a provider block supplying `apikey` with the secret in the environment is
reported as "environment `MESHSTACK_API_KEY` and `MESHSTACK_API_SECRET`". After 3 that case is
ordinary rather than exotic, so the origin has to be per setting and recorded by whichever function
resolved it.

```go
func (s *Session) Origins() []setting.Origin   // in the order of 2, appended as it resolves
```

**A slice, not a map**: one writer, and the order is the sequence in 2, which is the order a person
reading it wants. `ResolveSession` appends `Selection.Origins` and then its own. `auth.Status` drops
`Sources` for `Origins`, `meshstack profile view` prints them in order, and `meshstack auth status`
looks two up by key. **`Session` keeps plain value fields**: a struct of `setting.Resolved[T]` would
make every read carry an origin — `workspaces.go:40` would become `s.Endpoint.Value.URL` — to serve
two commands. Each `Origin.Source` comes from the winning source's own `Describe(key)`, so it cannot
drift from the value.

Also leaving `Session`: `whole` per 4, and `sources`.

**Three warnings, all `slog.WarnContext`**, and this is the whole set:

1. a profile picked by matching the endpoint;
2. `--api-secret-stdin` given while `MESHSTACK_API_SECRET` is also set;
3. the environment's secret differing from the profile's stored one.

**Accepted, and out of scope to fix:** terraform hides provider warnings unless the user sets
`TF_LOG` or `TF_LOG_PROVIDER`, so under the provider these three reach almost nobody. Promoting them
to terraform diagnostics would mean a second warning channel beside `slog`, and commit `8f05dce`
removed exactly that.

**The setting does not generate its own "not configured" error**, although it holds everything
needed. Only the middle clause of those messages is mechanical; what makes them good is specific —
which profile, why a secret is not a flag, which command lists the workspaces you may use. And the
reason to generate them does not hold: `pkg/auth` is not a front end, so it may read
`meshstack.Endpoint.EnvKey` in a hand-written sentence without breaking the rule that no front end
assembles a sentence out of an imported constant.

## 7. The front ends' sources

Each front end contributes exactly one `setting.Source`, and **a setting is identified by its
`EnvKey`**, so there is no second identifier to invent or hold in step.

```go
func (b blockSource) Lookup(key string) (string, bool) {
    switch key {
    case meshstack.Endpoint.EnvKey:   return value(b.data.Endpoint)
    case profile.Name.EnvKey:         return value(b.data.Profile)
    case meshstack.Workspace.EnvKey:  return value(b.data.Workspace)
    case credential.ApiKeyId.EnvKey:  return value(b.data.ApiKey)
    case credential.ApiSecret.EnvKey: return value(b.data.ApiSecret)
    case credential.ApiToken.EnvKey:  return value(b.data.ApiToken)
    }
    return "", false
}

func (blockSource) Describe(key string) string { return "provider block " + attribute(key) }
```

The same `switch` serves `Describe`, which is what makes an origin readable: the CLI's source
answers `--endpoint` where this one answers `provider block endpoint`.

The provider's `.golangci.yml` needs two more `allow` entries for this to compile at all. Its
`provider` rule is `list-mode: strict` and names four `meshstack-cli/pkg` packages;
`blockSource` adds `pkg/setting`, to implement the interface, and `pkg/profile`, for
`profile.Name.EnvKey`. Widening that list is a deliberate edit, because in both repositories
`depguard` is the dependency policy rather than an enforcement of one written elsewhere.

**`auth.Input` dissolves entirely**, and with it `auth.Values`, `SecretFromEnv`, `TokenFromEnv`,
`SecretPrompt`, `TokenPrompt`, `MissingSecretError` and `MissingTokenError`. Its two lazy accessors
existed so that a command served from a cached token never prompted; with prompting moved to
`cmd/auth/login.go` and the secret arriving in the source, reading it eagerly costs one `getenv`.
`providerInput` becomes `blockSource`, and the fallback at `auth_input.go:42` goes with it — the
block hands over its `apisecret` attribute and nothing else.

`pkg/auth/env.go` loses `envEndpoint`, `envApiKey`, `envApiSecret`, `envApiToken`, `envProfile`,
`source`, `describe`, `pick` and `env`. Every message that named one of those consts reads the
declaration's `EnvKey` instead, which is what 6 permits. `DefaultProfile` moves to `pkg/profile`;
`DevLocalProfile` stays, because it belongs to `--dev-local` alone.

## 8. `pkg/tty` loses its global

```go
var disabled atomic.Bool
func Disable() { disabled.Store(true) }
```

**This is a second precedence mechanism beside the one being built**, so it goes. `pkg/tty` shrinks
to the `NoInput` declaration, `IsTerminal(f)` and `NoInputHint()`. `MayInvolveAPerson()` collapses
into `!noInput`, and dropping `IsInteractive()` leaves each caller to compose
`!noInput && tty.IsTerminal(os.Stdin)` itself.

Two owners hold the resolved value, and they are the same declaration resolved from the same
sources, so they cannot disagree. `Session` gains an unexported `noInput` from step 5, which
`token.go:126` reads. `cmd/meshstack` resolves it once at startup into `cli.Input`, because
`askForEndpoint` has to decide whether it may prompt at a moment when `ResolveSession` has just
failed and there is no `Session` to ask.

`tty.NoInput` earns its place, though not for the reason its name suggests. Its main use is not the
prompt: `pkg/auth/login.go:145` uses it so a browser login fails at once instead of waiting ten
minutes for a callback nobody will complete. A terminal check cannot stand in — `meshstack login |
tee` and an agent driving the CLI both reach a person through stderr.

## 9. Messages: the static half in the domain package, the specifics in `pkg/auth`

This is the split the code already makes, chosen deliberately rather than inherited.
`pkg/meshstack`'s `ErrMissing` is a plain `errors.New`, and `token.go:109` wraps it as
`diags.Errorf("no workspace", "%s", meshstack.ErrMissing)`. The domain package owns what a workspace
is and which variable names it. Only the resolution knows which profile was picked, why, and whether
a prompt was possible.

It is also what keeps the declarations out of `pkg/setting`: `pkg/meshstack` naming
`MESHSTACK_WORKSPACE` in `ErrMissing` would make it import `pkg/setting`, while the endpoint
declaration needs `meshstack.ParseEndpoint` — a cycle. Keeping the static half in the domain package
avoids having to move all five messages into `pkg/auth` to break it.

## Behaviour changes

Three, all deliberate. **One provider CHANGELOG entry, describing the end state.** The provider's
CHANGELOG is not a history record yet, so rewrite it freely rather than adding a line per pull
request — a reader wants what the provider does now, not how the branch got there.

**An exported credential variable fixes the identity.** `MESHSTACK_API_KEY=k` alone, with a profile
holding a browser login, is ignored today and the browser login runs — the user acts under an
identity they did not ask for, with nothing said. It will instead fix the identity and fail, naming
the variable. `MESHSTACK_API_TOKEN` behaves the same way, except that it succeeds rather than
failing because a token needs no secret. The cost lands hardest on the provider, which cannot
prompt: a stale `MESHSTACK_API_KEY` fails a `terraform plan` that used to work. Chosen because
silently ignoring a deliberately exported credential variable is the worse failure — invisible, and
it changes who you are.

**A bare `--api-key` takes `MESHSTACK_API_KEY` over the profile's stored id.** Today the variable is
ignored there. It is the same argument as above, and it applies more strongly to `login`, which is
the command that writes the id to disk. The flag's help becomes "with the stored id,
MESHSTACK_API_KEY, or a new one".

**A command that is not `login` no longer prompts for a secret.** It errors instead, naming
`meshstack login --api-key=k`, per 5.

## Phase 3 — one lane

Not splittable: the ranked order, the credential unit rule and the origins are one function's
invariant. It is the phase that changes behaviour, so it carries the full unit suite, the CLI
acceptance suite, and a test for each row of 3's table and for each behaviour change.

**It owns `../terraform-provider-meshstack`.** That is a correction to the earlier boundary, not a
choice: dissolving `auth.Input` breaks `internal/provider/auth_input.go` on the same commit, so
`blockSource` has to land here for the provider to build at all. Phase 4 keeps everything that is
about presentation rather than about compiling.

Checkpoints, in order, each ending with `go build ./...`, the unit suite and `nix develop -c task
lint` green in both repositories:

1. `pkg/setting`'s `Origin`, `Environ`, `Default` and `DefaultFunc`, and the four `config.json`
   operations moved out of `pkg/auth/profiles.go`. Pure additions and a pure move, so nothing is
   implemented twice.
2. Everything else, in one checkpoint. `profile.Select` replaces `selectProfile` and `plainProfile`
   in the same commit that writes it; `ResolveSession` brings the order in 2, the credential unit
   rule in 3, the store in 4 and `Origins()`; `auth.Input` and `pkg/auth/env.go`'s consts go;
   `blockSource` replaces `providerInput`; and the front ends get the two stdin flags, the three
   sentinels, the prompts, `pkg/tty` without its global, origins in `auth status` and
   `profile view`, and the `strings.TrimSpace` sweep.

   The front ends cannot be a checkpoint of their own. `ResolveSession` reads the secret from the
   front end's source, so a commit that lands the resolution without the prompts is one where
   `meshstack login --api-key=k` cannot ask for a secret. Commit it in steps, but do not stop on a
   step that leaves a command worse than it was.

## Phase 4 — the provider, one lane

`Short` and `Long` reaching the schema — which closes a gap, since the provider's schema documents
no environment variable today — then `task generate`, the CHANGELOG rewrite, and the docs from 3:
the row 1 setup and the two messages that qualify it. Verification: the provider unit tests and its
acceptance suite.

`Short` is plain text and one line, for a cobra flag; `Long` is markdown, for the provider's schema,
and falls back to `Short` through `Help()`. The alignment is **adjacency, not derivation** — they
sit in one declaration, so changing a fact puts both in the same diff hunk. Shared text states facts
about the setting and names the environment variable; a sentence about how *this front end* exposes
it stays in the front end, with no "block", "flag" or "attribute" vocabulary in the shared part.

## Not in scope

- **Splitting the per-profile credentials file into a durable part and a token cache**, in two
  directories under `~/.config`. It is a real improvement — for the `apiKey` and `manual` methods the
  durable file would become write-once, where today an hourly renewal rewrites the file holding a
  client secret that never changes. It is deferred because it is orthogonal to resolving settings.
  - It does **not** shrink the lock, which is what it looks like it should do. `lock.go:20` says the
    lock exists because Keycloak rotates the refresh token on every refresh, and a refresh token is
    durable — so a `login`-method renewal writes both files and needs the lock over both.
  - It costs the single data model `pkg/profile` marshals, and it turns `login.go:156`'s deliberate
    one write of the refresh token and the unscoped access token together into two.
  - When it happens, both parts stay under `~/.config`. Access tokens are secrets, so `~/.cache`
    would spread secrets over two trees with different permission expectations; one tree at 0700 is
    easier to audit.
- A source cannot distinguish "set to empty" from "unset", so no setting can be deliberately cleared
  by an explicit empty value — `--workspace=""` cannot shed a profile's default. Nothing needs it.
- The memory store meaning two things ("not mine to persist" and "could not be persisted").
- The one-minute HTTP timeout against a backoff sized for four (`internal/http/http_client.go:23`).
- Restoring `ErrRefreshRejected` hints.
- Deleting the `requireDevLocalCredentials` skip guard once the meshfed change merges.
