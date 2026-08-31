# Plan — domain-driven settings, and one way to resolve them

One way to declare a configuration item, one way to resolve it, shared by the meshStack CLI and the
Terraform provider. Each item lives in the domain package it belongs to, carries its own
`MESHSTACK_*` name and its own help, and reports where its value came from.

Several decisions below contradict comments in the current code. Those are marked, because the
comment is what will otherwise make a reader think the change is a mistake.

## 1. A setting is a declaration beside the domain, not a domain type

`pkg/setting` holds the mechanism and nothing else:

```go
type Value[T any] struct {
    EnvKey string          // also the setting's identity — see 9
    Short  string          // plain text, one line, for a cobra flag
    Long   string          // markdown, for the provider schema; falls back to Short
    Parse  func(string) (T, error)
}
```

Generic over the domain type, so the parse step and its message live at the declaration.

**Four fields, and neither a source list nor a default**, although 3 gives every setting both. A
tier list on the declaration would teach `pkg/setting` the word "profile", and 6 puts that knowledge
in `pkg/auth` alone. Both therefore live at the resolution — see 3.

Declarations live in the domain packages, **without a `Setting` suffix**:

```
meshstack.Endpoint    meshstack.Workspace    profile.Name
credential.ApiKeyId   credential.ApiSecret   credential.ApiToken
tty.NoInput           profile.ConfigFile     profile.CredentialsDir
```

**`MESHSTACK_NO_BROWSER` is not among them.** `pkg/oidc/browser/browser.go:36` declares it as a
private const and reads it with `os.Getenv`; it never reaches `ResolveSession`, and 12 keeps it that
way. It stays an environment variable and gains no flag: `acceptance/acceptance_test.go:141` sets it
to run the suite, and `browser.go:31` records why it is not the same statement as
`MESHSTACK_NO_INPUT` — "do not launch a browser, but still print the URL and wait, because a person
is coming". A headless box reached over SSH is that case, and an export beats retyping a flag.

**They do not move into `pkg/setting`, although one package would read better at call sites.**
`meshstack.ErrMissing` names `MESHSTACK_WORKSPACE` in its text, so `pkg/meshstack` would import
`pkg/setting`; the endpoint declaration needs `meshstack.ParseEndpoint`. That is a cycle, and the
same shape repeats for `profile` and `credential`.

The package is `setting`, not `input` — meshStack's own vocabulary has building block inputs — and
not `config`, which here means the config file.

## 2. The mechanism declares and reads. It never decides.

```go
type Source interface {
    Lookup(key string) (string, bool)
    Name() string
}
```

**`Name()` is what keeps an origin from drifting from its value.** `setting.Resolve` returns the
winning source alongside the value, and the source that answered is the one that names itself — see
9. It also keeps 3's boundary: `pkg/setting` learns that a source has a name, not that profiles
exist, which is why an origin cannot be a typed `Kind` enum in this package.

**Text, not `T`, although the settings are generic.** The environment, argv and a Terraform
`types.String` can carry nothing else, and the profile file hands back `entry.Endpoint.String()`
losslessly because `encoding.TextMarshaler` is the format seam anyway. Nothing ever builds a
heterogeneous list of settings — each is resolved by name — so no non-generic `resolvable` interface
is needed.

**Two functions, and the plan means different ones in different places.** Naming them apart now:

```go
func setting.Resolve[T any](v Value[T], sources ...Source) (T, Source, error)   // one setting
func auth.ResolveSession(ctx context.Context, opts ResolveSessionOptions) (*Session, error)
```

`setting.Resolve` walks the sources it is handed and knows nothing about which they are.
`auth.ResolveSession` owns the per-setting ranked lists from 3, the credential rule from 4 and the
ordering from 6, and calls `setting.Resolve` once per setting. Where the sections below say
"`Resolve`" without a package, they mean `auth.ResolveSession`.

## 3. One ranked source list per setting

```
explicit → environment → profile → built-in default
```

`Resolve` walks the list in order and stops at the first hit, so a source below the winner is never
consulted. Laziness therefore needs no mechanism of its own.

**There is no interactive tier, because a prompt is not a source.** `Resolve` reports that it found
no value, and `cmd/auth/login.go` — the one command allowed to ask — prompts and calls `Resolve`
again with the answer as an explicit source. 6 already blesses that pattern for the endpoint. Four
settings can prompt, all four inside `meshstack login`: the endpoint, `credential.ApiKeyId`,
`credential.ApiSecret` and `meshstack.Workspace`.

**The workspace prompt is a different animal, and it is why the prompt cannot be a source.** It
offers a list fetched from meshStack, and `pkg/auth/workspaces.go:16` already builds that list from
an *unscoped* token: with no `c:` scope the principal holds only the list rights, which is exactly
what a user has before picking a workspace. So the prompt needs a finished `Session` — a resolved
endpoint, a resolved credential and a network call. It cannot run inside the resolution that produces
that session, and it does not: `Session.Workspaces` runs after `ResolveSession` returns.

This also makes "the provider never prompts" true because the provider never reaches the prompting
code, which is a stronger guarantee than the empty slice 10 used to hand it.

**What it costs:** `MESHSTACK_API_KEY=k` exported, no secret anywhere, at a terminal, running
`meshstack buildingblock list`. `internal/cli/secret.go` prompts today. It will error instead, naming
`meshstack login --api-key=k`. Accepted — a command that is not `login` should not open a login
dialogue.

**The list differs per setting, and it is named at the resolution rather than on the setting.**
`tty.NoInput` takes a flag and the environment but no profile — "never prompt" describes this
invocation, not this installation. `credential.ApiSecret`'s explicit source in the CLI is stdin
behind a flag, because a secret is never a flag *value*.

**`tty.NoInput` earns its place, though not for the reason its name suggests.** Its main use is not
the prompt: `tty.go:42`'s `MayInvolveAPerson()` is deliberately weaker than a terminal check, and
`pkg/auth/login.go:145` uses it so a browser login fails at once instead of waiting ten minutes for a
callback nobody will complete. A terminal check cannot stand in — `meshstack login | tee` and an
agent driving the CLI both reach a person through stderr. Its second use is `token.go:126`, where a
credentials file that cannot be written is fatal when interactive and silent when not.

`auth.ResolveSession` holds one ranked `[]Source` per setting, and that table is the only place a
ranking is visible. **The price is that those three facts are a convention inside one function, not
data a test can check across every declaration.** The alternative buys that test with the import
cycle in 1.

**The built-in default is the lowest-ranked source, not a field on the setting.** The list above
already ends with it, and text carries it without loss: a default path, or `false`. A field could not
express `profile.ConfigFile`, whose default calls `os.UserHomeDir()` and can fail. As a `Source` that
failure is `("", false)`, and the hand-written message from 9 says what is missing.

### Stdin is opt-in, through `--api-secret-stdin`

Docker, helm, `az acr`, crane and gh all gate stdin behind a flag, and one reason settles it here:
**with stdin implicit, "was a secret piped?" cannot be answered.** `!IsTerminal(stdin)` is true in
every CI job, every container without `-t` and every `< /dev/null`. The only way to tell is to read,
which consumes a line that may belong to the command and blocks forever on an open pipe nobody
writes to. So implicit stdin can neither outrank the environment nor report that it lost.

Behind a flag it is an explicit source, so it outranks the environment by the ranking above with no
special case. When `MESHSTACK_API_SECRET` is also exported, one `slog.WarnContext` names it as
ignored. **Not a hard error**, although docker makes the two-flag case one:
`gh auth login --with-token` exits 1 when `GH_TOKEN` is set, and that breaks exactly the CI runs
which export the variable on purpose.

This replaces the order in `internal/cli/secret.go:27`, whose doc comment justifies the environment
outranking the *prompt* and takes it outranking stdin along for the ride. `readLine` survives
unchanged, including its reason for reading exactly one line.

The cost is that `printf %s "$secret" | meshstack login --api-key=k` now needs the flag. Per 13 no
release tag exists, so no script breaks.

## 4. Pairing is the credential invariant

> **A credential resolves as a unit, in one walk down the ranked sources. The first source carrying
> an identity defines the credential. Its secret is its own secret slot when it has one; otherwise
> the first secret offered by a source that carries no identity of its own; otherwise nothing.**

The last clause is the one the earlier wording missed. Without it rows 1 and 4 are the same shape —
the block supplies the id, the block has no secret, the environment has one — and the table demanded
opposite answers.

| case | identity from | secret from | why |
|---|---|---|---|
| provider block `apikey`, `MESHSTACK_API_SECRET` | block | environment | the block has no `apisecret`, and the environment carries no id |
| `login --api-key=k`, secret prompted | flag | prompt | a flag never carries a secret, and nothing else offers one |
| profile holds `dev-key` + stored secret, `MESHSTACK_API_SECRET` exported | profile | the profile's own slot | its own slot wins before anything else is asked |
| block `apikey = "k"`, stale `MESHSTACK_API_KEY=j` + `MESHSTACK_API_SECRET=s` | block | nothing, so this fails | the environment carries a competing id, so its secret is skipped |

**Row 1 is the case to protect, not an edge case.** An id in the provider block, or on `--api-key`,
with the secret in the environment is the normal non-interactive setup. It must keep working, and an
acceptance test on both front ends should say so.

**The exclusion in row 4 fires against one source and no other.** Only a source that offers an id of
its own is skipped, and that is the environment — the profile always stores its id beside its secret,
and a prompt carries neither. So the cost is one `Lookup` and one branch.

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

**`MESHSTACK_API_TOKEN` counts as an identity in the walk**, and so does a profile's `Manual`. A token
names who you are as much as a key id does. It matters most for the exclusion clause: without it, a
source holding a token would sit quietly and still be allowed to hand its secret to another source's
`apikey`.

**A token needs no secret**, so the second half of the unit rule does not run for it.

**One source carrying both a token and a key id is an error**, not a precedence contest. They are two
different methods rather than two spellings of one thing, so choosing silently hands the user an
identity they did not pick — the failure the behaviour changes below refuse to accept. The message
follows row 4's shape, naming both and saying to remove one.

The case is narrow: the CLI has no `--api-token` flag, because a token is a secret and 3 keeps
secrets out of flag values. Only the environment and the provider block can hold both.

### Row 3 warns. It does not rewrite the profile.

Row 3 costs the rotation case: the user exports a rotated secret, the profile keeps serving the old
one, and meshStack answers 401 with nothing said. **The fix is a warning, not a write.**

**Rejected: have the resolution store the environment's secret into the profile.** Three reasons.

- **Which secret is newer is not knowable.** The opposite case is at least as common — a stale
  `MESHSTACK_API_SECRET` in a shell beside a profile logged in an hour ago. Overwriting there
  destroys a working credential, and unsetting the variable afterwards does not bring it back. A 401
  is recoverable; this is not.
- **It makes a read path write.** `pkg/profile/lock.go:20` puts the lock around a *grant*, which is
  rare. A write during resolution makes every command contend for it, including each resource in a
  `terraform plan`.
- **"Fail if the update was not possible" turns a cache-write failure into a command failure.** A
  read-only home directory in a container is ordinary and does not stop a working credential today.

The write belongs in `meshstack login`, which already writes and where the user has said to make this
their credential.

So: keep the profile's paired secret, and when the environment carries a different one, warn.

> `MESHSTACK_API_SECRET` is set and differs from the secret stored for `dev-key` in profile `dev`.
> The stored one is being used. To replace it, run
> `meshstack login --api-key=dev-key --api-secret-stdin`.

Skip the comparison when the profile stores a `SecretCommand` rather than a literal — comparing would
mean running that command during a resolution.

**This contradicts `token.go:335-343` and its comment**, which says the environment sits above the
profile for the secret. That is a bug: with `MESHSTACK_API_SECRET` exported and a profile holding a
different key, the call becomes `apiLogin(dev-key, ci-secret)` and meshStack answers 401. The
`!stored` half of that condition is right and survives; the `env(envApiSecret) != ""` half goes.

It also removes `wholeCredential` and the `forLogin` bypass in `resolve`. **`ResolveForLogin` exists
only because "one source supplies the whole credential" was wrong for `auth login`** — construction
by the identity's source needs no exception. Confirm against `cmd/auth/login.go` before deleting.

And it removes the reason for the comment at `auth_input.go:42` ("The fallback is not a second
precedence rule"). The provider hands over its `apisecret` attribute and nothing else;
`auth.SecretFromEnv` and `auth.TokenFromEnv` stop being exported.

## 5. `pkg/credential`, top level, one closed sum

Not under `profile` — a credential from the environment touches no file. Not under `auth` —
`pkg/profile` must import it, and reaching into another package's subtree is the wart that forced
`pkg/auth/method` to exist. It absorbs that package.

```go
type Method string   // "login" | "apiKey" | "manual"

type Credential struct {
    Current Method  `json:"current,omitzero"`
    Login   *Login  `json:"login,omitzero"`
    ApiKey  *ApiKey `json:"apiKey,omitzero"`
    Manual  *Manual `json:"manual,omitzero"`
}

type ApiKey struct {
    Id            string      `json:"clientId"`
    Secret        string      `json:"clientSecret,omitzero"`
    SecretCommand []string    `json:"clientSecretCommand,omitzero"`
    AccessToken   IssuedToken `json:"accessToken,omitzero"`
}
```

`pkg/profile` marshals this directly — no second data model.

### The token cache belongs to the method that minted it

`credentials.go:26` puts one `AccessTokens map[scope.Scope]IssuedToken` on the whole credential. That
cannot survive this section's own decision: a profile holding a login **and** an API key would mix
tokens from two methods in one key space, so switching `Current` could hand out a token the API key
never minted. Today `resolve.go:45` guards that at runtime with `current`, "the method every cached
token belongs to", read under a mutex — a guard that exists only because the file shape cannot say
who a cached token belongs to.

So the cache goes on the method, and **only `Login` needs a map**:

```go
type Login  struct { …; AccessTokens map[scope.Scope]IssuedToken `json:"accessTokens,omitzero"` }
type ApiKey struct { …; AccessToken  IssuedToken                 `json:"accessToken,omitzero"`  }
type Manual struct { …; AccessToken  IssuedToken                 `json:"accessToken,omitzero"`  }
```

`token.go:83` already states why: a browser login mints a user access token bound to one workspace,
so it needs one token per `c:` scope plus the unscoped one that `Session.Workspaces` uses. "An API
key or a pasted token carries whatever workspace its issuer put in it, and nothing re-scopes one", so
`Session.Scope()` returns `workspace.Unscoped` for both. A single field says that; a one-entry map
only implies it.

The single token *is* the unscoped token — the field name carries it and no scope key is written.
Making the file uniform instead, with `{"accessToken": {"": …}}`, would need a custom marshaller to
render one token as a one-entry map, which is the opposite of the simplification.

Mixing is now impossible rather than guarded, so **check in phase 3 whether `resolve.go:45`'s
`current` is still needed as a cache guard.** `store.go:121` and `store.go:179` prune expired tokens
and now walk whichever of the three shapes is present.

**It holds more than one credential at a time**, so that `meshstack login` switches back from an API
key without asking for the id again. `Current` is the selection; the pointers are the set.
**Presence is not selection**: always switch on `Current`, never on a nil check.
`if c.Login != nil { return ErrMissing }` would wrongly demand a workspace from a profile minting
with its API key.

**One type for the file and for the resolution, although they are different shapes.** A credential
resolved from the environment, a flag or a provider block is exactly one method, so it carries two
nil pointers and only `Current` means anything. A closed sum — an interface with a marker method —
would make "presence is not selection" impossible to get wrong instead of a rule to remember, and it
is still not worth two conversions and turning six branches into type switches.

The rule is held up where holding it is cheap:

- **Constructors, so that no caller assembles the struct.** `credential.FromLogin`,
  `credential.FromApiKey` and `credential.FromManual` each set `Current` and its pointer together.
- **One invariant check, called by the constructors and after unmarshalling:** `Current` is
  non-empty, and the pointer it names is non-nil. A hand-edited profile file then fails with a
  message rather than resolving to a credential nobody selected.
- A test over all three methods that the check rejects every mismatched pair.

**Minting stays in `pkg/auth`**, with one three-arm switch. `Mint` on the type would pull
`internal/http` and `pkg/oidc` — and through it the whole API client — into `pkg/profile`, which
reads and writes two files.

**No behaviour methods on the type**, although they look natural. Every branch on the method already
lives in `pkg/auth` (six) or `cmd/` (two, both presentation); `pkg/profile` branches zero times. Only
`ApiKey.Resolve(ctx)` and `Method.Description()` stay — they are about the shape, not about which
method is current.

An ephemeral secret from HCL, the environment or a prompt lands in the same `Secret` field that gets
marshalled. It stays out of the file because such a credential gets a memory store, which cannot
write. **That guarantee is structural and load-bearing** — check it before touching the store split.

## 6. Sources may require settings

`Endpoint` resolves from the profile; `profile.Name` resolves partly from an endpoint match.

> **A source may itself require settings. Those settings resolve from the sources ranked above it,
> and never from the source being built.**

Two properties this leans on, both for the package doc:

- **Adding a lower-ranked source can never change an already-resolved value.** This is what makes
  resolving the endpoint twice a technique rather than a bug.
- **A prompt never happens inside a resolution.** Per 3 it cannot: `cmd/auth/login.go` prompts for an
  endpoint *before* re-resolving and feeds it in as explicit. That is what keeps a source that is
  still being built from asking a question about itself.

The **ordering** lives in `auth.ResolveSession`. `pkg/setting` never learns that profiles exist.

### The order, written out

Three of these passes deliberately use a shorter list than the setting's full one, so the sequence
has to be explicit rather than left to the prose above.

| # | resolve | from |
|---|---|---|
| 1 | `profile.ConfigFile`, `profile.CredentialsDir` | explicit → environment → default |
| 2 | `Endpoint`, first instalment | explicit → environment |
| 3 | `profile.Name` | explicit → environment → endpoint match on (2) → `"default"` |
| 4 | the profile source itself | read with (1) and (3) |
| 5 | `Endpoint`, second instalment | the profile, but only when (2) found nothing |
| 6 | the credential, as one unit per 4 | explicit → environment → profile |
| 7 | `Workspace` | explicit → environment → profile; no default |
| 8 | `tty.NoInput` | explicit → environment → `false` |

**A missing workspace is not always an error.** It is needed only when the credential method is
`login`, because a meshStack user access token is bound to one workspace while an API key carries its
own — `provider.go:60` already documents that. When it is needed and missing:

- `meshstack login` mints the unscoped token, calls `Session.Workspaces`, and prompts from the list.
  This happens after `ResolveSession` returns, per 3, and it is what the CLI does today.
- Every other command, and the provider, error and name the command that lists the workspaces.

The rename in 7 carries `workspace.Unscoped` to `meshstack.Unscoped`, and dropping `workspace.Name`
per 8 makes `Session.Workspaces` return `[]string`.

Step 1 comes first because nothing can read a profile before the paths to it exist. Step 2 drops the
profile tier because the profile is what it is being used to pick, and step 3 skips the match source
when step 2 found nothing.

**Steps 2 and 5 are one list evaluated in two instalments, not two resolutions.** `Endpoint`'s list
is explicit → environment → profile throughout; step 2 evaluates the prefix that is available, and
step 5 evaluates the one remaining source only when the prefix came back empty. Re-reading explicit
and environment in step 5 could not change the answer — they are the same sources that already said
nothing — so the property "adding a lower-ranked source can never change an already-resolved value"
is not a claim to check here, it is why the second instalment is a single lookup.

`Endpoint` has no built-in default, so a missing endpoint after step 5 is an error.

This deletes the copy of profile selection at `cmd/auth/login.go:200-206`, which `askForEndpoint`
uses to guess which profile resolution would pick — and which is already stale: it omits
`MESHSTACK_PROFILE` and the endpoint match, so `MESHSTACK_PROFILE=staging meshstack login` prompts
about the wrong profile.

## 7. `pkg/meshstack` holds names, not objects

`pkg/workspace` renamed, and it gains `parseEndpoint`, `sameEndpoint` and `canonicalEndpoint` from
`pkg/auth/env.go` — facts about an endpoint, not about authentication.

**It does not wrap `client.MeshWorkspace`.** That type carries `tfsdk:` tags and is the provider's
state model, so embedding it produces a second workspace type the provider cannot use. And `client/`
may not import `pkg/`, so a `Name()` accessor there could not return a typed name.
`meshstack.Workspace(ws.Metadata.Name)` at the call site is fine.

## 8. Named types only where they are a shape or a key

**Dropped, against the domain-driven instinct:** `workspace.Name`, and `profile.Name` as a type.
Plain `string` matches `client.MeshWorkspaceMetadata.Name`, cobra flag targets and
`types.String.ValueString()` with no cast at any boundary, and the type prevented no confusion those
boundaries did not already require an explicit conversion for.

**Kept:** `xurl.URL` and `jwt.JWT` (parsed forms), `credential.Method` (switched on), and
**`scope.Scope` above all** — it keys `map[scope.Scope]IssuedToken`, so as a plain string, indexing
that cache with a workspace name instead of `c:<workspace>` would compile and fail *silently*. Every
other confusion here produces a wrong message a test catches; this one produces a wrong token.

`Name.Scope()` becomes `meshstack.WorkspaceScope(name string) scope.Scope`.

## 9. Origins and help, but no generated errors

`pkg/auth/resolve.go:41`'s `sources map[string]string` goes. It is assembled at eight call sites and
is sometimes wrong: a provider block supplying `apikey` with the secret in the environment is
reported as "environment `MESHSTACK_API_KEY` and `MESHSTACK_API_SECRET`". After 4 that case is
ordinary rather than exotic — a credential's id and its secret legitimately come from different
sources — so the origin has to be per setting and recorded by the function that resolves.

```go
type Origin struct {
    Key    string  // the setting's EnvKey, its identity per 10
    Source string  // "--endpoint", "MESHSTACK_ENDPOINT", "profile dev", "built-in default"
}

func (s *Session) Origins() []Origin   // in resolution order, appended by ResolveSession
```

**A slice, not a map**: one writer, and the order is the sequence in 6, which is the order a person
reading it wants. **`Session` keeps plain value fields.** A struct of `setting.Resolved[T]` would
make every read carry an origin — `workspaces.go:40` would become `s.Endpoint.Value.URL` — to serve
two commands. The `Source` name comes from `Source.Name()` per 2, so it cannot drift from the value.

Also leaving `Session`: `whole bool`, since 4 removes `wholeCredential`; `Workspace workspace.Name`
becomes `string` per 8; `current method.Method` becomes `credential.Method`.

`pkg/auth` emits one debug record per resolution. **Three warnings, all `slog.WarnContext`**, and
this list is the whole set:

1. a profile picked by matching the endpoint;
2. `--api-secret-stdin` given while `MESHSTACK_API_SECRET` is also set (3);
3. the environment's secret differing from the profile's stored one (4).

**Accepted, and out of scope to fix:** terraform hides provider warnings unless the user sets
`TF_LOG` or `TF_LOG_PROVIDER`, so under the provider these three reach almost nobody. Promoting them
to terraform diagnostics would mean a second warning channel beside `slog`, and commit `8f05dce`
removed exactly that. One plain `slog` interface is worth more than the reach.

`Short` is required and plain; `Long` is markdown, optional, falls back to `Short`. In practice the
CLI renders `Short` and the provider renders `Long`, so the alignment is **adjacency, not
derivation** — they sit in one declaration, so changing a fact puts both in the same diff hunk.
Shared text states facts about the setting and names the environment variable; a sentence about how
*this front end* exposes it stays in the front end, with no "block", "flag" or "attribute" vocabulary
in the shared part. This closes a gap: the provider's schema documents no environment variable today.

**The setting does not generate its own "not configured" error**, although it holds everything
needed. Only the middle clause of those five messages is mechanical; what makes them good is specific
— which profile, why a secret is not a flag, which command lists the workspaces you may use. And the
reason to generate them does not hold: `pkg/auth` is not a front end, so it may read
`meshstack.Endpoint.EnvKey` in a hand-written sentence without breaking the rule that no front end
assembles a sentence out of an imported constant. `EnvKey` therefore becomes a readable field rather
than a private const.

## 10. The front end contributes one source

**No `Frontend` interface.** Once 3 moved the prompt into `cmd/auth/login.go`, the interface had one
method returning one value — which is a struct field:

```go
type ResolveSessionOptions struct {
    Settings setting.Source   // the CLI's flags and --api-secret-stdin, or the provider block
}

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (*Session, error)
```

**An options struct rather than a bare `setting.Source` argument**, so that a later addition — a
second source, a clock, an injected store — is a new field instead of a signature change both front
ends have to follow. A nil `Settings` is a front end contributing nothing explicit, not an error.

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
```

**A setting is identified by its `EnvKey`.** The rule that every setting gets a `MESHSTACK_*` name
earns a second keep: there is no second identifier to invent or hold in step.

"The provider never prompts" is no longer a promise any interface has to carry: per 3 the prompt
lives in `cmd/auth/login.go`, which the provider does not link.

## 11. The browser is an argument, not a capability on the interface

```go
func (s *Session) LoginWithBrowser(ctx context.Context, open Browser) (LoginResult, error)
```

`pkg/auth/login.go:134` is the only place a browser is reached; a dead login errors rather than
re-opening one. So `Input.Browser()`, `providerInput.Browser()` returning nil, its test, and the nil
check inside `pkg/auth` all go, along with the comment at `provider.go:122` that explains them.

**A setting cannot replace this**, although `MESHSTACK_NO_BROWSER` makes it look like one. To disable
a browser at runtime the provider would first have to *link* the browser flow, trading a compile-time
guarantee that `.golangci.yml` enforces for a boolean somebody could get wrong.

## 12. `pkg/tty` loses its global

```go
var disabled atomic.Bool
func Disable() { disabled.Store(true) }
```

**This is a second precedence mechanism beside the one being built**, so it goes. All three callers
already have an owner for the resolved value: `internal/cli/secret.go` and `cmd/auth/login.go` hold
the CLI's `Input`, and `pkg/auth/token.go:126` holds the `Session`.

`pkg/tty` shrinks to the `NoInput` declaration, `IsTerminal(f)` and `NoInputHint()`.
`MayInvolveAPerson()` collapses into `!noInput`. Dropping `IsInteractive()` leaves `token.go:126` to
compose `!noInput && tty.IsTerminal(...)` itself. The concept stays — see the note in 3 on what
`tty.NoInput` is actually for. `MESHSTACK_NO_BROWSER` is declared in `pkg/oidc/browser`, the only
package that may read it, and is not a setting at all per 1.

## 13. `encoding/json/v2` replaces goccy

It is in the Go 1.27 standard library as a real importable package — no `GOEXPERIMENT`, verified
against go1.27.0, and `go.mod:5` already pins 1.27. Dropping `github.com/goccy/go-yaml` takes the
module to the two external dependencies `.golangci.yml` already claims as the policy.

**Known and accepted: the import costs the provider's users the `nojsonv2` opt-out.** Go 1.27 keeps
`GOEXPERIMENT=nojsonv2` for code that breaks under the new implementation, and it only builds when
nothing in the tree imports v2 — [the request to lift that](https://go.dev/issue/79788) was closed as
not planned. This module is a library the Terraform provider imports, so a direct v2 import removes
the hatch for the provider and for anything importing the provider. Chosen anyway: the hatch is
transitional and Go says it will be removed, and v2's strict parsing is worth having on a file a user
may edit by hand — v1 accepts `{"b":"x","b":"y"}` and silently keeps `"y"`, while v2 rejects a
duplicate object name and rejects invalid UTF-8 in a string.

**Strictness is the reason for v2, and it is a reason on its own.** Plain `encoding/json` would have
carried every mechanical need below — v1 has had `omitzero` since Go 1.24 and has always honoured
`TextMarshaler` for values — so a config file that reports its own corruption instead of quietly
picking a value is what the import buys. If this is ever revisited, that is the thing to defend.

- **Each tag option is chosen per field. There is no mechanical `yaml:` → `json:` rename.** In v2
  `omitempty` omits an *encoded* empty value (`""`, `{}`, `[]`, `null`), while `omitzero` omits the
  Go zero value or anything whose `IsZero()` reports true.
  - **`omitzero`** where the Go zero value means "not set": every `time.Time`, every `*xurl.URL`,
    every pointer in the credential sum, and `Method`.
  - **No option at all** where the zero value has to round-trip, starting with `Version` — a file
    must never lose the field that says how to read it.
  - **`omitempty`** only where an encoded empty value is the absent one. In practice that is
    `SecretCommand []string`, and `omitzero` says the same thing more precisely there, so
    `omitempty` may end up unused.
  - The step-1 test pins this per shape: marshal a zero value, marshal a fully populated one, and
    unmarshal both back.
- `xurl.URL` needs nothing: it already implements `encoding.TextMarshaler` and `TextUnmarshaler`.
- Write with `jsontext.WithIndent`, so the file stays readable by hand.

**A hard break.** No release tag exists, so no installed CLI has files that must keep loading. Rename
to `config.json` and `credentials/<profile>.json`, leave old YAML unread, keep `Version` at 1. About
ten doc comments name `config.yaml`.

## Behaviour changes

Two, both deliberate. **One provider CHANGELOG entry, describing the end state.** The provider's
CHANGELOG is not a history record yet, so rewrite it freely rather than adding a line per pull
request — a reader wants what the provider does now, not how the branch got there.

**An exported credential variable fixes the identity.** `MESHSTACK_API_KEY=k` alone, with a profile
holding a browser login, is ignored today and the browser login runs — the user acts under an
identity they did not ask for, with nothing said. It will instead fix the identity and fail, naming
the variable. `MESHSTACK_API_TOKEN` behaves the same way, per the token rule in 4, except that it
succeeds rather than failing because a token needs no secret. The cost lands hardest on the provider,
which cannot prompt: a stale `MESHSTACK_API_KEY` fails a `terraform plan` that used to work. Chosen
because silently ignoring a deliberately exported credential variable is the worse failure —
invisible, and it changes who you are.

**The file format changes**, per 13. A user with an existing profile runs `meshstack login` again.

## Landing order

The order is chosen so that each phase is safe to hand to somebody working on their own, and so that
a phase's lanes can run at the same time. **Containing the user-visible break is not a goal** — there
is no release, so it costs nothing to land it early. **The design above wins over these boundaries.**
If implementing a step shows a decision is wrong, revisit the decision rather than bending the code
to fit the step.

### Two rules that make parallel work safe

- **One lane at a time may edit `../terraform-provider-meshstack`**, and the table names which. The
  provider pins this branch, so a CLI push without the matching provider edit leaves the provider not
  building. Every phase boundary is therefore one CLI push plus one provider pin bump, together.
- **Every lane ends green**: `task lint`, `go build ./...`, the unit suite, and the provider still
  builds. A lane that cannot reach that state stops and says so rather than handing on a red tree.

### Phase 1 — the moves, one lane, owns the provider

Steps 2 and 3 of the old order, together, plus the empty mechanism.

| does | why here |
|---|---|
| `pkg/credential`: absorb `pkg/auth/method`, move the three shapes out of `pkg/profile` | pure move |
| `pkg/meshstack`: rename `pkg/workspace`, drop the named types per 8, move `ParseEndpoint`, `sameEndpoint` and `canonicalEndpoint` out of `pkg/auth/env.go` | pure rename |
| `pkg/setting`: the package, `Value[T]`, `Source`, `setting.Resolve` — no declarations yet | tiny, and it unblocks every phase-2 lane |

Both moves land in one lane because both edit the provider's `auth_input.go`, so splitting them buys
a merge conflict and a second pin bump. Verification: everything compiles, the unit suite is
unchanged, the provider builds.

### Phase 2 — three lanes at once

| lane | does | touches | provider |
|---|---|---|---|
| A | json/v2 and the file format, per 13 | `pkg/profile` I/O, the tags on the moved shapes | none |
| B | the declarations | one new file per domain package: `meshstack`, `credential`, `profile`, `tty`, `browser` | none |
| C | decision 11, the browser as an argument | `pkg/auth/login.go`, `internal/cli`, `provider.go:122` | **owns it** |

A and B meet only in `pkg/credential`, and in different files — A edits struct tags, B adds a
declarations file. C is the one lane touching the provider in this phase.

Lane B's own test: every `Short` non-empty and backtick-free, and every `EnvKey` unique across all
declarations. Lane A's: the round trip per shape from 13.

### Phase 3 — `auth.ResolveSession`, one lane

Step 5, and decision 12 with it — `pkg/tty` can only lose its global once a `Session` owns the
resolved value. This is the phase that changes behaviour, so it carries the full unit suite, the CLI
acceptance suite, and a test for each row of 4's table and each behaviour change.

Not splittable: the ranked table, the credential unit rule and the origins are one function's
invariant.

### Phase 4 — the provider, one lane

Step 6: `blockSource`, the aligned `Short`/`Long` text reaching the schema, `task generate`, the
CHANGELOG rewrite, and the docs from 4 — the row 1 setup and the two messages that qualify it.
Verification: provider unit tests and the provider acceptance suite.

## Open questions

1. **Does `auth.Values` survive in any form?**
2. **Where do the five "not configured" messages live** once `pkg/auth/env.go` dissolves? Some belong
   to `pkg/meshstack`, some to `pkg/credential`, some stay.

Answered during the review: `Frontend`'s name (10 deletes it), `Session` and origins (9), whether
`auth status` reports them (yes, 9), `--no-browser` as a flag (no, and `MESHSTACK_NO_BROWSER` stays
an environment variable — 1 and 12), and where `AccessTokens` lives (5 — on the method, and only
`Login` needs a map).

## Not in scope

- The memory store meaning two things ("not mine to persist" and "could not be persisted").
- Path A holding a store it does not need.
- The one-minute HTTP timeout against a backoff sized for four (`internal/http/http_client.go:23`).
- Restoring `ErrRefreshRejected` hints.
- Deleting the `requireDevLocalCredentials` skip guard once the meshfed change merges.
