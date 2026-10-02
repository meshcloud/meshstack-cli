package auth_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

const (
	workersPerSession = 5
	requestsPerRound  = 3
	revokeEvery       = 50 * time.Millisecond
)

// TestConcurrentSessionsShareOneMintedToken resolves sessions from three goroutines while five
// more per session send authorized requests at once, and revokes tokens underneath all of them.
// No request may fail, no request may carry a token the backend never minted, and a session of
// five concurrent callers may mint only once.
func TestConcurrentSessionsShareOneMintedToken(t *testing.T) {
	quietLogging(t)
	server := newTestServer(t)

	// The warm-up writes profiles.json, the credentials file and the token cache before any
	// goroutine starts. A lock file whose directory does not exist yet cannot be taken, and
	// internal/lock reports that as acquired, so the first writer would otherwise be unguarded.
	warmUp, storeWarmUp, unlock, err := auth.Login(t.Context(), credential.ApiKeyName, sessionOptsFor(testApiKey1))
	require.NoError(t, err)
	requireAnswer(t, server, warmUp)
	require.NoError(t, storeWarmUp(t.Context()))
	require.NoError(t, unlock())

	resolvers := []*stressResolver{
		{name: "first-key-1", apiKey: testApiKey1, every: 100 * time.Millisecond},
		{name: "second-key-1", apiKey: testApiKey1, every: 100 * time.Millisecond},
		// The second api key writes its own identity into the one cache file both keys share,
		// so the resolvers above find a cache minted for someone else and have to mint again.
		{name: "only-key-2", apiKey: testApiKey2, every: 300 * time.Millisecond},
	}

	// A round workCtx cuts off is not a failure, which is what the ctx.Err() checks below allow for,
	// and it does not count as completed either.
	workCtx, endWork := context.WithTimeout(t.Context(), stressDuration(t))
	defer endWork()

	var (
		failures       stressFailures
		storing        sync.Mutex
		roundsInFlight atomic.Int64
		running        sync.WaitGroup
	)
	for _, resolver := range resolvers {
		running.Go(func() {
			resolver.run(t, workCtx, server, &storing, &roundsInFlight, &failures)
		})
	}
	running.Go(func() {
		revokeTokens(t, workCtx, server, &roundsInFlight)
	})
	running.Wait()

	failures.requireNone(t)

	var started, completed int64
	for _, resolver := range resolvers {
		assert.Positive(t, resolver.completed.Load(), "%s never completed a round", resolver.name)
		started += resolver.started.Load()
		completed += resolver.completed.Load()
	}

	counts := server.Counts()
	t.Logf("%d rounds started, %d completed, %+v", started, completed, counts)

	assert.Zero(t, counts.UnknownTokens, "every request carried a token this server had minted")
	assert.Positive(t, counts.RevokedTokens, "no revocation reached a session still using the token")
	assert.GreaterOrEqual(t, counts.Authorized, completed*workersPerSession*requestsPerRound,
		"every worker of every completed round got its answers")
	// One mint for the warm-up, one per started session at most, and one per rejected request at most.
	assert.LessOrEqual(t, counts.Logins, 1+started+counts.RevokedTokens,
		"a session minted more than once without having been rejected")
	// The bound above still allows one mint per round, so this is the tighter check: five
	// concurrent callers share one token, and so do the sessions that follow them.
	assert.Less(t, counts.Logins, counts.Authorized/10,
		"the token cache saved far fewer logins than it should have")
}

// stressResolver runs one session per tick, which bounds the live goroutines while still
// overlapping every other resolver's rounds.
type stressResolver struct {
	name   string
	apiKey fakemeshstack.ApiKey
	every  time.Duration

	started   atomic.Int64
	completed atomic.Int64
}

func (r *stressResolver) run(t *testing.T, ctx context.Context, server *fakemeshstack.Server, storing *sync.Mutex, inFlight *atomic.Int64, failures *stressFailures) {
	t.Helper()
	ticker := time.NewTicker(r.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// The count covers resolution and store as well, because revoking anywhere in that
		// span would defeat the one retry. See revokeTokens.
		r.started.Add(1)
		inFlight.Add(1)
		session, err := auth.ResolveSession(ctx, sessionOptsFor(r.apiKey))
		answered := false
		if err == nil {
			answered = r.askConcurrently(t, ctx, server, session, failures)

			// Storing is serialized across resolvers because concurrent writers of one profile
			// are not something the CLI has to support, while concurrent authorization is.
			storing.Lock()
			err = storeByLogin(ctx, r.apiKey)
			storing.Unlock()
		}
		inFlight.Add(-1)

		if err != nil {
			if ctx.Err() == nil {
				failures.add(fmt.Errorf("%s round %d: %w", r.name, r.started.Load(), err))
			}
			return
		}
		if !answered {
			return
		}
		r.completed.Add(1)
	}
}

func (r *stressResolver) askConcurrently(t *testing.T, ctx context.Context, server *fakemeshstack.Server, session auth.Session, failures *stressFailures) bool {
	t.Helper()
	// Closing the channel releases every worker in the same instant, so they all reach the
	// freshly resolved session's empty cache together.
	release := make(chan struct{})
	var cutShort atomic.Bool
	var workers sync.WaitGroup
	for worker := range workersPerSession {
		workers.Go(func() {
			<-release
			for range requestsPerRound {
				if err := ask(ctx, server, session); err != nil {
					if ctx.Err() == nil {
						failures.add(fmt.Errorf("%s worker %d: %w", r.name, worker, err))
					}
					cutShort.Store(true)
					return
				}
			}
		})
	}
	close(release)
	workers.Wait()
	return !cutShort.Load()
}

// revokeTokens stops honoring the token the next session will find in the cache file, so that
// its five callers all meet a 401 at once and go through auth.Session.RefreshBearerToken
// together. Exactly one of them may then mint, which is what the login count checks.
//
// It revokes only between rounds, as [fakemeshstack.Server.RevokeNewestToken] requires.
func revokeTokens(t *testing.T, ctx context.Context, server *fakemeshstack.Server, inFlight *atomic.Int64) {
	t.Helper()
	ticker := time.NewTicker(revokeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if inFlight.Load() == 0 {
				server.RevokeNewestToken()
			}
		}
	}
}

// stressFailures collects what went wrong on the worker goroutines, where require must not be
// called.
type stressFailures struct {
	mu    sync.Mutex
	count int
	first []error
}

func (f *stressFailures) add(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if len(f.first) < 10 {
		f.first = append(f.first, err)
	}
}

func (f *stressFailures) requireNone(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Zerof(t, f.count, "%d calls failed, the first of them: %v", f.count, errors.Join(f.first...))
}

// sessionOptsFor supplies the api key as a front end setting source rather than through the
// environment. The resolvers run at the same time, and one process cannot hold two values of
// MESHSTACK_API_KEY at once.
func sessionOptsFor(key fakemeshstack.ApiKey) auth.ResolveSessionOptions {
	opts := testSessionOpts
	opts.SettingSources = setting.Sources{
		setting.FrontendSource{Source: staticSetting(auth.ApiKeyClientIdSetting.EnvKey(), key.ClientId)},
		setting.FrontendSource{Source: staticSetting(auth.ApiKeyClientSecretSetting.EnvKey(), key.ClientSecret)},
	}
	return opts
}

func staticSetting(envKey, value string) setting.Source {
	return setting.LookupSource{
		MatchingKey: envKey,
		Description: "the stress test",
		Func: func(_ context.Context) (string, error) {
			return value, nil
		},
	}
}

// storeByLogin stores what a session resolved the only way there is, which is a login.
func storeByLogin(ctx context.Context, key fakemeshstack.ApiKey) error {
	_, store, unlock, err := auth.Login(ctx, credential.ApiKeyName, sessionOptsFor(key))
	if err != nil {
		return err
	}
	return errors.Join(store(ctx), unlock())
}

func stressDuration(t *testing.T) time.Duration {
	t.Helper()
	if testing.Short() {
		return 1 * time.Second
	}
	return 10 * time.Second
}

func quietLogging(t *testing.T) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
}
