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

Declarations live in the domain packages, **without a `Setting` suffix**:

```
meshstack.Endpoint    meshstack.Workspace    profile.Name
credential.ApiKeyId   credential.ApiSecret   credential.ApiToken
tty.NoInput           browser.NoBrowser      profile.ConfigFile   profile.CredentialsDir
```

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
}
```

**Text, not `T`, although the settings are generic.** The environment, argv and a Terraform
`types.String` can carry nothing else, and the profile file hands back `entry.Endpoint.String()`
losslessly because `encoding.TextMarshaler` is the format seam anyway. Nothing ever builds a
heterogeneous list of settings — `Resolve` calls each by name — so no non-generic `resolvable`
interface is needed.

## 3. Two source tiers

```
passive     (ranked, safe to read eagerly)   explicit → environment → profile → built-in default
interactive (ordered, lazy, last resort)     stdin when not a terminal → prompt
```

The interactive tier sits *below* the built-in default: you prompt exactly when there is no value at
all. It is never read during `Resolve`, because reading it blocks and can fail.

**Each setting declares its own source list.** `tty.NoInput` takes a flag and the environment but no
profile — "never prompt" describes this invocation, not this installation. `browser.NoBrowser` takes
the environment only. `credential.ApiSecret` has no explicit tier in the CLI, because a secret is
never a flag value.

## 4. Pairing is the credential invariant

> **A credential is constructed by the source that supplies its identity. For the secret, that source
> tries its own slot first, then the shared declared chain.**

| case | outcome |
|---|---|
| provider block `apikey`, `MESHSTACK_API_SECRET` | block has no `apisecret`, so the chain supplies it |
| `login --api-key=k`, prompted secret | a flag never carries a secret, so the chain supplies it |
| profile holds `dev-key` + stored secret, `MESHSTACK_API_SECRET` exported | the profile's own slot wins; the environment is never consulted |
| block `apikey = "k"`, stale `MESHSTACK_API_KEY=j` + `MESHSTACK_API_SECRET=s` | `s` belongs to `j` and is never paired with `k` |

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
    Id            string   `json:"clientId"`
    Secret        string   `json:"clientSecret,omitzero"`
    SecretCommand []string `json:"clientSecretCommand,omitzero"`
}
```

`pkg/profile` marshals this directly — no second data model.

**It holds more than one credential at a time**, so that `meshstack login` switches back from an API
key without asking for the id again. `Current` is the selection; the pointers are the set.
**Presence is not selection**: always switch on `Current`, never on a nil check.
`if c.Login != nil { return ErrMissing }` would wrongly demand a workspace from a profile minting
with its API key.

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
- **A setting that selects a source has no interactive tier.** This already holds:
  `cmd/auth/login.go` prompts for an endpoint *before* re-resolving and feeds it in as explicit.

The **ordering** lives in `pkg/auth.Resolve`. `pkg/setting` never learns that profiles exist.

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

`Resolve` returns the value and its origin, replacing `Session.sources map[string]string` — which is
assembled at eight call sites and is sometimes wrong: a provider block supplying `apikey` with the
secret in the environment is reported as "environment `MESHSTACK_API_KEY` and `MESHSTACK_API_SECRET`".
`pkg/auth` then emits one debug record per resolution. The one warning — a profile picked by matching
the endpoint — stays a `slog.WarnContext`.

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

## 10. The front end contributes sources

```go
type Frontend interface {
    Settings() setting.Source     // flags, or the provider block
    Secrets() []setting.Source    // stdin then a prompt; the provider contributes none
}
```

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

"The provider never prompts" stops being a promise and becomes an empty slice.

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
`MayInvolveAPerson()` collapses into `!noInput`. `MESHSTACK_NO_BROWSER` is declared in
`pkg/oidc/browser`, the only package that may read it.

## 13. `encoding/json/v2` replaces goccy

It is in the Go 1.27 standard library as a real importable package — no `GOEXPERIMENT`. Dropping
`github.com/goccy/go-yaml` takes the module to the two external dependencies `.golangci.yml` already
claims as the policy.

- **`omitempty` does not mean what it meant.** In v2 it omits an *encoded* empty value (`""`, `{}`,
  `[]`, `null`); `omitzero` omits the Go zero value or anything whose `IsZero()` is true. Every
  `time.Time` and every `*xurl.URL` wants `omitzero`. A mechanical `yaml:` → `json:` rename would
  start writing `"expiresAt": "0001-01-01T00:00:00Z"`.
- `xurl.URL` needs nothing: it already implements `encoding.TextMarshaler` and `TextUnmarshaler`.
- Write with `jsontext.WithIndent`, so the file stays readable by hand.

**A hard break.** No release tag exists, so no installed CLI has files that must keep loading. Rename
to `config.json` and `credentials/<profile>.json`, leave old YAML unread, keep `Version` at 1. About
ten doc comments name `config.yaml`.

## Behaviour changes

Two, both deliberate, both needing a provider CHANGELOG entry.

**A partial credential fixes the identity.** `MESHSTACK_API_KEY=k` alone, with a profile holding a
browser login, is ignored today and the browser login runs — the user acts under an identity they did
not ask for, with nothing said. It will instead fix the identity and fail, naming the variable. The
cost lands hardest on the provider, which cannot prompt: a stale `MESHSTACK_API_KEY` fails a
`terraform plan` that used to work. Chosen because silently ignoring a deliberately exported
credential variable is the worse failure — invisible, and it changes who you are.

**The file format changes**, per 13. A user with an existing profile runs `meshstack login` again.

## Landing order

This order exists to keep each change reviewable, and for no other reason. **The design above wins
over these boundaries.** If implementing a step shows a decision is wrong, revisit the decision
rather than bending the code to fit the step.

Steps 1 to 4 change no behaviour, so only step 5 needs the careful test pass and the acceptance run.

| # | step | touches | provider | verification |
|---|---|---|---|---|
| 1 | json/v2 and the file format | `pkg/profile` | none | unit tests; `go.mod` drops goccy |
| 2 | `pkg/credential` | absorbs `pkg/auth/method`, moves the three shapes out of `pkg/profile` | a renamed import | unit tests |
| 3 | `pkg/meshstack` | renames `pkg/workspace`, drops the named types, moves `ParseEndpoint` | `auth_input.go` only | unit tests |
| 4 | `pkg/setting` and the declarations | new package, declarations in domain packages; `Resolve` still hand-written | none | unit tests; new test asserting every `Short` is non-empty and backtick-free, and every `EnvKey` unique |
| 5 | `Resolve` rewritten over sources | `pkg/auth` | none yet | full unit suite, CLI acceptance suite, both behaviour changes covered |
| 6 | `blockSource` and aligned descriptions | — | `auth_input.go`, `provider.go`, `task generate` | provider unit + acceptance; CHANGELOG entry |

Decisions 11 and 12 ride along with step 5: both need the resolved value to reach a `Session`.

## Open questions

1. **How does `Session` carry the resolved settings?** A struct of `setting.Resolved[T]`, or plain
   fields plus a separate origin map? Decides what `auth status` and `profile view` can show.
2. **Does `profile.Stored` keep `AccessTokens` inside the credential or beside it?** A cache is not a
   credential, which argues for beside — at the price of a second top-level field.
3. **What is `Frontend` actually called**, and does `auth.Values` survive in any form?
4. **Where do the five "not configured" messages live** once `pkg/auth/env.go` dissolves? Some belong
   to `pkg/meshstack`, some to `pkg/credential`, some stay.
5. **Should `--no-browser` exist as a flag?** Deliberately excluded here.
6. **Should `auth status` report origins?** Two lines, and it would let `auth status` answer "which
   profile, and why", which today only `meshstack profile view` does.

## Not in scope

- The memory store meaning two things ("not mine to persist" and "could not be persisted").
- Path A holding a store it does not need.
- The one-minute HTTP timeout against a backoff sized for four (`internal/http/http_client.go:23`).
- Restoring `ErrRefreshRejected` hints.
- Deleting the `requireDevLocalCredentials` skip guard once the meshfed change merges.
