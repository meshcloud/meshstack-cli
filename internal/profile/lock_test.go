package profile

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/config"
)

func lockIsFree(t *testing.T, dir string) bool {
	t.Helper()
	probe := flock.New(filepath.Join(dir, "profiles.json.lock"))
	taken, err := probe.TryLock()
	require.NoError(t, err)
	if taken {
		require.NoError(t, probe.Close())
	}
	return taken
}

var exclusively = LoadProfilesOptions{ExclusiveLock: true}

func TestTheExclusiveLockOfTheProfiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-yet")
	t.Setenv(config.DirectorySetting.EnvKey(), dir)

	t.Run("is held next to profiles.json until Unlock", func(t *testing.T) {
		profiles, err := LoadProfiles(t.Context(), exclusively)

		require.NoError(t, err)
		assert.DirExists(t, dir, "the configuration directory is created, as there is no lock without it")
		assert.False(t, lockIsFree(t, dir))
		profiles.add(Profile{Name: "dev"})
		require.NoError(t, profiles.Store(t.Context()), "the holder stores under its own lock")
		require.NoError(t, profiles.Unlock())
		assert.True(t, lockIsFree(t, dir), "Unlock returns once the lock is free")
		require.NoError(t, profiles.Unlock(), "a second Unlock releases nothing")
	})

	t.Run("fails another exclusive load after lockWaitTime, and lets a load that only reads go ahead at once", func(t *testing.T) {
		held, err := LoadProfiles(t.Context(), exclusively)
		require.NoError(t, err)
		defer func() { require.NoError(t, held.Unlock()) }()

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			_, err := LoadProfiles(t.Context(), exclusively)
			require.ErrorIs(t, err, ErrInUse, "a load that takes it fails")
			assert.Equal(t, lockWaitTime, time.Since(start))

			start = time.Now()
			profiles, err := LoadProfiles(t.Context(), LoadProfilesOptions{})
			require.NoError(t, err, "a load without ExclusiveLock only reads, and takes no lock")
			assert.Zero(t, time.Since(start), "and does not wait")
			require.NoError(t, profiles.Unlock(), "Unlock does nothing for it")
		})
		assert.False(t, lockIsFree(t, dir), "the other command still holds the lock")
	})

	t.Run("is released where ResolveProfile fails", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "missing")

		_, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{StoredOnly: true, ExclusiveLock: true})

		require.ErrorIs(t, err, ErrNoStoredProfile)
		assert.True(t, lockIsFree(t, dir))
	})

	t.Run("is released where the profiles are invalid", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "profiles.json"), []byte(`{"version":0}`), 0o600))

		_, err := LoadProfiles(t.Context(), exclusively)

		require.ErrorContains(t, err, "version in")
		assert.True(t, lockIsFree(t, dir))
	})
}
