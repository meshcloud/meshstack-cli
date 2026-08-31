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

## Phase 2

## Phase 3

## Phase 4
