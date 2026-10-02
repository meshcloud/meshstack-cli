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

func TestLoadProfilesWithExclusiveLockHoldsTheLockNextToProfilesJsonUntilUnlock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-yet")
	t.Setenv(config.DirectorySetting.EnvKey(), dir)

	profiles, err := LoadProfiles(t.Context(), exclusively)

	require.NoError(t, err)
	assert.DirExists(t, dir, "the configuration directory is created, as there is no lock without it")
	assert.False(t, lockIsFree(t, dir))
	Add(&profiles, Profile{Name: "dev"})
	require.NoError(t, profiles.Store(t.Context()), "the holder stores under its own lock")
	require.NoError(t, profiles.Unlock())
	assert.True(t, lockIsFree(t, dir), "Unlock returns once the lock is free")
	require.NoError(t, profiles.Unlock(), "a second Unlock releases nothing")
}

func TestLoadProfilesWithExclusiveLockFailsWhileAnotherCommandHoldsTheLock(t *testing.T) {
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	held, err := LoadProfiles(t.Context(), exclusively)
	require.NoError(t, err)
	defer func() { require.NoError(t, held.Unlock()) }()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		_, err := LoadProfiles(t.Context(), exclusively)

		require.ErrorIs(t, err, ErrInUse)
		assert.Equal(t, lockWaitTime, time.Since(start))
	})
}

func TestLoadProfilesWithoutExclusiveLockTakesNoLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), dir)
	held, err := LoadProfiles(t.Context(), exclusively)
	require.NoError(t, err)
	defer func() { require.NoError(t, held.Unlock()) }()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		profiles, err := LoadProfiles(t.Context(), LoadProfilesOptions{})

		require.NoError(t, err, "a load without ExclusiveLock only reads, while another command holds the lock")
		assert.Zero(t, time.Since(start), "and does not wait")
		require.NoError(t, profiles.Unlock(), "Unlock does nothing for it")
	})
	assert.False(t, lockIsFree(t, dir), "the other command still holds the lock")
}

func TestLoadProfilesWithExclusiveLockReleasesItWhereTheProfilesAreInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "profiles.json"), []byte(`{"version":0}`), 0o600))

	_, err := LoadProfiles(t.Context(), exclusively)

	require.ErrorContains(t, err, "version in")
	assert.True(t, lockIsFree(t, dir))
}

func TestResolveProfileWithExclusiveLockReleasesItWhereItFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), dir)

	_, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{StoredOnly: true, ExclusiveLock: true})

	require.ErrorIs(t, err, ErrNoStoredProfile)
	assert.True(t, lockIsFree(t, dir))
}
