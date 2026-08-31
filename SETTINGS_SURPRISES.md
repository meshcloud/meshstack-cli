# Surprises while implementing `SETTINGS_PLAN.md`

Things the plan got wrong, left out, or could not have known. One entry per surprise, newest
section last, each naming the decision it revises and what was decided instead. The plan stays as
written — this file is the diff against reality.

## Phase 1

### `parseEndpoint` is in `resolve.go`, not `env.go`

7 says `pkg/meshstack` gains `parseEndpoint`, `sameEndpoint` and `canonicalEndpoint` "from
`pkg/auth/env.go`". Only the last two are there. `parseEndpoint` is `pkg/auth/resolve.go:339`.
No consequence beyond where to look.

### The credential method constants collide with the credential struct types

5 asks for both `Method` constants named `Login`, `ApiKey` and `Manual` (absorbed from
`pkg/auth/method`) and struct types of exactly those names, in one package. Go allows one
`credential.ApiKey`.

**Decided:** the constants become `credential.MethodLogin`, `credential.MethodApiKey` and
`credential.MethodManual`; the struct types keep the plain names. A call site then reads
`switch c.Current { case credential.MethodApiKey: … }` against `c.ApiKey`, which is clearer than
either half of the collision would have been.

### The three endpoint helpers have to be exported to move

7 writes them lowercase, but `pkg/auth` is their only caller and it is a different package after
the move. `ParseEndpoint` and `SameEndpoint` become exported; `canonicalEndpoint` stays private,
because `SameEndpoint` is its only user.

### `pkg/oidc/jwt` holds a workspace claim, so dropping `workspace.Name` reaches it

8 drops the type. `pkg/oidc/jwt/claim.go:15` declares `WorkspaceClaim` as `Claim[workspace.Name]`
keyed by `workspace.ClaimKey`, so it becomes `Claim[string]` keyed by `meshstack.ClaimKey` and
`pkg/oidc/jwt` gains an import of `pkg/meshstack`. Allowed — `.golangci.yml`'s `pkg` rule permits
any `pkg/…` import — and no cycle, because `pkg/meshstack` imports only `pkg/oidc/scope`.

### The per-method token cache cannot wait for phase 2

The landing order puts the token cache on the method in phase 2 lane A, beside the JSON switch.
`Manual` has no content at all without its cached token, so "move the shapes" and "move the
cache" are not two steps.

**Decided:** the cache moved in phase 1, with the shapes. Lane A keeps the format break and the
JSON switch, and the two lanes then touch disjoint files.

### So the file format is not byte-identical after phase 1

Phase 1 was meant to be a pure move. It cannot be: `currentMethod` becomes `current`, `methods:`
disappears into the inlined credential, and the top-level `accessTokens:` map moves under
`login:`. 13 breaks the format anyway and there is no release, so nothing is lost — but a phase 1
tree already needs `meshstack login` run again, and `store_test.go`'s two literal-YAML fixtures
changed with it.

`goccy/go-yaml` does honour `yaml:",inline"` on an embedded struct in both directions.
`TestCredentialsRoundTrip` pins it.

### `ApiKeyMethod.Secret` has to be renamed, not just moved

5 calls it `ApiKey.Resolve` without saying why. The reason is that `ClientSecret` becomes
`Secret` in the same move, and Go allows one `ApiKey.Secret`.

### The constructors cannot call `Validate`

5 asks for `Validate` to be "called by the constructors and after unmarshalling", but
`FromLogin` and friends return a `Credential` and no error. Each sets `Current` and its pointer
together, so there is nothing for the check to find and nothing to do with a failure.

**Decided:** the constructors hold the invariant by construction, and `Validate` is called from
`profile.fileStore.Read` alone. `credential_test.go` pins both halves.

### `Validate` makes `currentMethod`'s method-presence fallback unreachable

`resolve.go` fell back to "no current method, but a login is stored, so use it". A credential
holding a method and naming none is exactly what `Validate` now rejects, and a profile's
credential only reaches `currentMethod` through `Read`.

**Decided:** the two `slog.Info` fallback arms are gone. The remaining default — an empty
credential resolves to `login` — stays. It also means a file with a method but no `current` now
fails with a message instead of being guessed at, which is the point of 5's invariant.

### `switchTo`: a login updates a credential, it does not build one

5 says no caller assembles a `Credential`, but `meshstack login` is not assembling one — it is
replacing one method of an existing set while keeping the others, which is what makes switching
back cost one refresh. A bare `credential.FromApiKey(...)` at those call sites would delete the
stored browser login.

**Decided:** `pkg/auth/login.go` gains `switchTo(previous, selected)`. Every call site still
builds `selected` with a constructor; `switchTo` carries the replaced methods over without their
cached tokens, which is what the shared token map used to do by being overwritten.

### The provider's depguard needs `pkg/credential` spelled out

`pkg/auth/method` was reachable from `internal/provider` only because
`github.com/meshcloud/meshstack-cli/pkg/auth` is a prefix of it. At the top level it needs its
own `allow` entry in the provider's `.golangci.yml`. The CLI's own `pkg` rule already covers it.

### `Long` falls back to `Short`, and nothing implemented the fallback

1 and 9 both state the rule, but a four-field struct gives no front end anywhere to get it, so
both would have written `if Long == "" { Long = Short }`.

**Decided:** `setting.Value.Help()` returns Long, or Short where a declaration wrote only the one.

### A source cannot distinguish "set to empty" from "unset"

`setting.Resolve` skips a source that answers `""`, because every source in this module reports
an unset value that way and an unset flag must not silence the environment below it. The cost is
that no setting can be deliberately cleared by an explicit empty value — `--workspace=""` cannot
shed a profile's default. 3 does not discuss the case. Left as it is; nothing needs it yet.

### `workspace.Name.Empty()` trimmed, so a plain `== ""` is a different check

8 drops the type and every `Empty()` with it. The method was
`strings.TrimSpace(string(n)) == ""`, so today a `--workspace "  "` counts as no workspace at
all: `RequireWorkspace` refuses it with `ErrMissing`, and a profile's whitespace-only
`defaultWorkspace` is ignored. Writing `== ""` at the call sites would change that, and the
`Scope()` half of it silently — the guard would pass while the scope stayed `Unscoped`, which
is the wrong-token failure 8 keeps `scope.Scope` to avoid.

**Decided:** the call sites read `strings.TrimSpace(x) == ""`, so the lane changes no
behaviour. It is verbose, and `resolve.go` now reads
`if ws == "" && strings.TrimSpace(entry.DefaultWorkspace) != ""` because the untrimmed half was
already written that way. Collapsing all of them to `== ""` is a one-line-per-site follow-up,
but it is a behaviour change in whitespace-only input and needs to be decided as one.

It resolves in phase 3 rather than as its own decision: `meshstack.Workspace` is a
`setting.Value[string]` parsed with `setting.Text`, which trims. Once every workspace reaches a
call site through `ResolveSession`, the value is already trimmed and `== ""` is the same check
the method was. Delete the `strings.TrimSpace` calls then, not before.

### `pkg/meshstack` imports three packages, not one

The entry above says there is no cycle "because `pkg/meshstack` imports only `pkg/oidc/scope`".
With `ParseEndpoint` it also imports `pkg/diags` and `client/types/xurl`. Still no cycle: all
three of those import nothing from this module. `.golangci.yml`'s `pkg` rule allows
`client` and `pkg` wholesale, so `pkg/oidc/jwt` importing `pkg/meshstack` needs no rule change.

### Dropping the type makes `WorkspaceClaim`'s converter redundant

`Claim.GetFrom` already type-asserts to `V` when `converter` is nil, so `Claim[string]` with a
converter that asserts to `string` does the assertion twice. `WorkspaceClaim` is now
`Claim[string]{key: meshstack.ClaimKey}`, the same shape as `UsernameClaim`.

### `pkg/auth` writes `MESHSTACK_WORKSPACE` as a literal

15 rests on "no `MESHSTACK_*` name is exported, and every message naming one is produced in the
package that consults it". `resolve.go:86` breaks the second half already: it labels the
workspace's origin for `meshstack profile view` with the literal `"MESHSTACK_WORKSPACE"`, so
`pkg/auth` names a variable it cannot see. Nothing in phase 1 needs it fixed — the rule holds
for every *message*, and this is a label — and 2's `Source.Describe` is what removes it. Left
for the lane that adds the declarations.

### The provider stops importing the package the rename was about

`workspace.Name` was the only thing `internal/provider` took from `pkg/workspace`, so dropping
the type leaves the provider importing nothing from `pkg/meshstack`. Its `.golangci.yml` still
carried an `allow` entry for the old path.

**Decided:** the entry is renamed rather than deleted, so the policy says the same thing it said
before the rename — the provider may reach the domain names. Deleting it would be a narrowing,
and phase 4 puts the schema text in the declarations that live there.

## Phase 2

### 1's list of non-settings is missing `MESHSTACK_SKIP_VERSION_CHECK`

1 names `MESHSTACK_NO_BROWSER` as the one `MESHSTACK_*` variable that is not a setting.
`client/mesh_info.go:87` reads a second one, and it cannot become a `setting.Value` at all:
`.golangci.yml`'s `client` rule allows `$gostd`, `client/` and `internal/http` and nothing else,
so `client/` may not import `pkg/setting`. Lane B leaves it where it is; "one way to declare a
configuration item" means one way for everything under `pkg/`.

### A declaration with a nil `Parse` panics inside `Resolve`

Generics give `setting` no way to default it, so the guard belongs in lane B's test over every
declaration, beside "every `Short` non-empty" and "every `EnvKey` unique".

### 13 does not say what an existing user's `config.yaml` produces

The rename is `config.yaml` → `config.json` and `Version` stays at 1, so neither the name nor the
version field tells an old file from a new one. Where nothing points at the old file the reader
simply does not find it, which is fine. Where `MESHSTACK_CONFIG_FILE` names it explicitly — which
the Terraform provider's `scratch/run.sh` does — the reader gets a raw JSON syntax error, and
"Behaviour changes" promises the user a sentence telling them to run `meshstack login` again.

**Decided:** no format detection. Lane A's parse-error message checks whether the file starts with
something other than `{` and, when it does, says the format changed and to log in again.

## Phase 3

These four came out of reading the Terraform provider's `scratch/` demos against the plan. None
of them blocks an earlier phase, and all four have to be settled before `auth.ResolveSession` is
written.

### 6 step 4 opens files that 3 promises are never consulted

3 says a source below the winner is never consulted, so "laziness needs no mechanism of its own".
Step 4 builds the profile source before step 6 walks the credential, and building it means
reading `credentials/<profile>.json` — a file a run whose credential came wholly from the
environment never touches today. `resolve.go:183`'s "this credential belongs to a different
meshStack" check sits behind exactly the short-circuit step 4 removes, so `scratch/run.sh env` on
a machine whose default profile points at another meshStack would start failing.

**Decided:** the profile is two sources, not one. A config source over `config.json` answers the
endpoint match and the default workspace, and is what step 4 builds; a credentials source over
`credentials/<profile>.json` is consulted only in step 6. Both open their file on first use.

5's "a credential that arrived whole gets a memory store" is computed from `Session.whole`, which
9 deletes. What replaces it: the credential's winning source was not the credentials source.

### 4's "check both callers before deleting `ResolveForLogin`" undercounts

There are four commands, not two: `cmd/auth/login.go:148` and `:158`, `cmd/auth/logout.go:27`,
`cmd/profile/set.go:38`, plus `pkg/auth/devlocal.go:33`.

`cmd/profile/set.go` is the one that breaks. Its comment says it resolves "the way a login is" so
that "a shell that exports a credential must not make it write nothing" — and with 14 reserving
`DemandMethod` to `cmd/auth/login.go`, an exported `MESHSTACK_API_KEY` would make
`meshstack profile set workspace x` resolve to a memory store and silently write nothing. `logout`
has the same shape. Both need a way to reach the profile store regardless of which source won the
credential, and the plan has to say what it is.

### 6 reads as if the missing-workspace rule lives in the resolution

"Every other command, and the provider, error and name the command that lists the workspaces"
sits inside the step 1 to 8 table. The check is `Session.RequireWorkspace()` at `token.go:107`,
called by `provider.go:125` and deliberately **not** by `cmd/workspace/list.go` or
`cmd/auth/status.go` — the two commands the error message itself tells the user to run. Folding it
into `ResolveSession` makes the escape hatch the message names unreachable.

**Decided:** step 7 resolves and never demands. `RequireWorkspace` survives as a post-resolution
call.

### `--api-secret-stdin` does not fit `setting.Source`

2's `Lookup(key string) (string, bool)` has neither a context nor an error return, and
`input.go:52` makes the secret deliberately lazy so that a command served from a cached token
never prompts. A stdin source consulted during resolution reads even when no minting follows, and
a blocked read has nowhere to report itself.

**Decided:** `cmd/` reads stdin once, before `ResolveSession`, and contributes the result as a
plain string in the CLI's own source. `Source` stays synchronous and pure, and the read happens
where a context and an error return exist. A stored `SecretCommand` stays lazy behind
`credential.ApiKey.Resolve(ctx)`, which 4 already requires.

### 3's "no script breaks" defends the wrong risk

The risk is not a script that pipes a secret on purpose. It is one that hands the CLI a
non-terminal stdin by accident: `scratch/headless-login.sh` runs it with `</dev/null`, so today a
`--api-key` there reads an empty secret from `/dev/null` and gets a silent 401. 3's change is
right, and this is the reason to give for it.

## Phase 4
