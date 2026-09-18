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
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testserver"
)

const (
	workersPerSession = 5
	greetingsPerRound = 3
	revokeEvery       = 50 * time.Millisecond
)

// TestSessionConcurrentStressTest resolves sessions from three goroutines while five more per
// session send authorized requests at once, and revokes tokens underneath all of them. What it
// is looking for is a token cache that holds up: no request may fail, no request may carry a
// token the backend never minted, and a session of five concurrent callers may mint only once.
func TestSessionConcurrentStressTest(t *testing.T) {
	quietLogging(t)
	server := newTestServer(t)

	// The warm-up writes profiles.json, the credentials file and the token cache before any
	// goroutine starts. A lock file whose directory does not exist yet cannot be taken, and
	// internal/lock reports that as acquired, so the first writer would otherwise be unguarded.
	warmUp := requireSession(t, testApiKey1)
	server.RequireGreeting(t, greetingClient(warmUp))
	require.NoError(t, warmUp.Store(t.Context()))

	resolvers := []*stressResolver{
		{name: "first-key-1", apiKey: testApiKey1, every: 100 * time.Millisecond},
		{name: "second-key-1", apiKey: testApiKey1, every: 100 * time.Millisecond},
		// The second api key writes its own identity into the one cache file both keys share,
		// so the resolvers above find a cache minted for someone else and have to mint again.
		{name: "only-key-2", apiKey: testApiKey2, every: 300 * time.Millisecond},
	}

	// Everything below runs on workCtx, so a round it cuts off is not a failure: see the ctx.Err() checks.
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

	var rounds int64
	for _, resolver := range resolvers {
		assert.Positive(t, resolver.rounds.Load(), "%s never completed a round", resolver.name)
		rounds += resolver.rounds.Load()
	}

	counts := server.Counts(t)
	t.Logf("%d rounds, %+v", rounds, counts)

	assert.Zero(t, counts.UnknownTokens, "every request carried a token this server had minted")
	assert.Positive(t, counts.RevokedTokens, "no revocation reached a session still using the token")
	assert.GreaterOrEqual(t, counts.Greetings, rounds*workersPerSession*greetingsPerRound,
		"every worker of every completed round got its greetings")
	// One mint for the warm-up, one per session at most, and one per rejected request at most.
	assert.LessOrEqual(t, counts.Logins, 1+rounds+counts.RevokedTokens,
		"a session minted more than once without having been rejected")
	// The bound above still allows a mint per round. This is the cache actually working: five
	// concurrent callers share one token, and so do the sessions that follow them.
	assert.Less(t, counts.Logins, counts.Greetings/10,
		"the token cache saved far fewer logins than it should have")
}

// stressResolver resolves a session of its own on every tick and drives workersPerSession
// goroutines against it at once. A session lives for exactly one round, which bounds the live
// goroutines and the request rate while still overlapping every other resolver's rounds.
type stressResolver struct {
	name   string
	apiKey testserver.ApiKey
	every  time.Duration

	rounds atomic.Int64
}

func (r *stressResolver) run(t *testing.T, ctx context.Context, server *testserver.Server, storing *sync.Mutex, inFlight *atomic.Int64, failures *stressFailures) {
	t.Helper()
	ticker := time.NewTicker(r.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// The count covers the whole round, resolution and store included, because that is the
		// span in which revoking a token would defeat the one retry. See revokeTokens.
		inFlight.Add(1)
		session, err := auth.ResolveSession(ctx, sessionOptsFor(r.apiKey))
		if err == nil {
			r.greetConcurrently(t, ctx, server, session, failures)

			// Storing is serialized across resolvers because concurrent writers of one profile
			// are not something the CLI has to support, while concurrent authorization is.
			storing.Lock()
			err = session.Store(ctx)
			storing.Unlock()
		}
		inFlight.Add(-1)

		if err != nil {
			if ctx.Err() == nil {
				failures.add(fmt.Errorf("%s round %d: %w", r.name, r.rounds.Load()+1, err))
			}
			return
		}
		r.rounds.Add(1)
	}
}

func (r *stressResolver) greetConcurrently(t *testing.T, ctx context.Context, server *testserver.Server, session auth.Session, failures *stressFailures) {
	t.Helper()
	greet := greetingClient(session)
	// Closing the channel releases every worker in the same instant, so they all reach the
	// freshly resolved session's empty cache together.
	release := make(chan struct{})
	var workers sync.WaitGroup
	for worker := range workersPerSession {
		workers.Go(func() {
			<-release
			for range greetingsPerRound {
				if err := server.Greeting(t, ctx, greet); err != nil {
					if ctx.Err() == nil {
						failures.add(fmt.Errorf("%s worker %d: %w", r.name, worker, err))
					}
					return
				}
			}
		})
	}
	close(release)
	workers.Wait()
}

// revokeTokens stops honoring the token the next session will find in the cache file, so that
// its five callers all meet a 401 at once and go through auth.Session.RefreshBearerToken
// together. Exactly one of them may then mint, which is what the login count checks.
//
// It only revokes between rounds. A revocation during a request can leave a 401 that nothing
// recovers from, because an authorized client retries once and the token it retries with may be
// one it adopted from the cache file a moment before that token was revoked. That is the limit
// of the single retry, not a property worth asserting on.
func revokeTokens(t *testing.T, ctx context.Context, server *testserver.Server, inFlight *atomic.Int64) {
	t.Helper()
	ticker := time.NewTicker(revokeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if inFlight.Load() == 0 {
				server.RevokeNewestToken(t)
			}
		}
	}
}

// stressFailures collects what went wrong on the worker goroutines, where require would be
// undefined behaviour. It keeps the first few errors in full and counts the rest.
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
func sessionOptsFor(key testserver.ApiKey) auth.ResolveSessionOptions {
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

func requireSession(t *testing.T, key testserver.ApiKey) auth.Session {
	t.Helper()
	session, err := auth.ResolveSession(t.Context(), sessionOptsFor(key))
	require.NoError(t, err)
	return session
}

func stressDuration(t *testing.T) time.Duration {
	t.Helper()
	if testing.Short() {
		return 1 * time.Second
	}
	return 10 * time.Second
}

// quietLogging keeps the discarded-cache warning and the per-resolution info line out of the
// output, because this test triggers each of them a few hundred times.
func quietLogging(t *testing.T) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
}
