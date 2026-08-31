package profile

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

// Store owns one profile's credentials, including its lock. Both front ends use it: the
// Terraform provider reads a profile like the AWS provider reads ~/.aws, and it has to
// write rotated refresh tokens back. The memory implementation satisfies the same
// interface with no lock and no file.
type Store interface {
	// Read returns the credentials without taking the lock.
	Read() (Credentials, error)

	// Update runs mint while holding the exclusive lock, having re-read first so that a
	// process which lost the race sees the winner's token instead of minting again.
	// Whatever mint returns is written in one atomic operation, which is what keeps a
	// rotated refresh token and the access token it came with from ever being separated.
	Update(ctx context.Context, mint func(Credentials) (Credentials, error)) (Credentials, error)

	// Forget removes the profile's credentials file.
	Forget() error

	// Describe names where the credentials live, for `meshstack auth status` and for an
	// error that has to say which file it means. A memory store says so rather than
	// naming a path.
	Describe() string

	// Writable reports whether Update can persist. A memory store is not writable, and
	// pkg/auth uses this to decide whether a profile degraded to memory.
	Writable() bool
}

// ErrNotWritable reports that a file store could not persist: the lock could not be taken,
// or the atomic write failed. pkg/auth matches on it to degrade a profile to a memory store,
// which is how a read-only home directory stays usable.
//
// It exists because Writable cannot be answered honestly in advance: a probe would create a
// file a read-only command must not create, and could still disagree with the later write.
// Reporting the real failure is exact where a probe is a guess.
var ErrNotWritable = errors.New("this profile's credentials could not be written")

// fileStore is one profile's `credentials/<profile>.yaml` plus the lock next to it.
type fileStore struct {
	path     string
	lockPath string
}

// NewFileStore resolves and validates the path of a profile's credentials file. It
// creates nothing: a process that only reads leaves no trace, and the directory appears
// the first time something is actually stored.
func NewFileStore(name string) (Store, error) {
	path, err := CredentialsPath(name)
	if err != nil {
		return nil, err
	}
	return &fileStore{path: path, lockPath: path + lockSuffix}, nil
}

func (s *fileStore) Describe() string { return s.path }

// Writable is true for a file store by construction. Whether the write really succeeds
// is answered by attempting it, not by probing beforehand, because a probe both creates
// something a read-only command should not create and can disagree with the later write.
func (s *fileStore) Writable() bool { return true }

// Read returns empty credentials when the file is missing, because a profile in
// config.yaml with no credentials file means "not logged in" — the same state as a
// fresh install, and not an error.
func (s *fileStore) Read() (Credentials, error) {
	creds := Credentials{Version: Version}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return creds, nil
	}
	if err != nil {
		return Credentials{}, diags.Wrap(err, "Cannot read the stored credentials",
			"%s could not be read.", s.path)
	}
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return Credentials{}, diags.Wrap(err, "Cannot parse the stored credentials",
			"%s is not valid YAML: %v", s.path, err)
	}
	if err := checkVersion(creds.Version, s.path); err != nil {
		return Credentials{}, err
	}
	// Checked here rather than at the point of use, so that the message can name the file.
	if err := creds.Validate(); err != nil {
		return Credentials{}, diags.Wrap(err, "Cannot use the stored credentials",
			"%s does not describe a usable credential: %v", s.path, err)
	}
	return creds, nil
}

func (s *fileStore) Update(ctx context.Context, mint func(Credentials) (Credentials, error)) (Credentials, error) {
	release, err := acquireLock(ctx, s.lockPath)
	if errors.Is(err, ErrLockBusy) {
		return Credentials{}, err
	}
	if err != nil {
		return Credentials{}, errors.Join(err, ErrNotWritable)
	}
	defer release()

	// Re-read under the lock: another process may have renewed while this one waited, and
	// mint is expected to notice that and hand the credentials back unchanged.
	current, err := s.Read()
	if err != nil {
		return Credentials{}, err
	}
	next, err := mint(current)
	if err != nil {
		return Credentials{}, err
	}
	next.Version = Version
	next.Credential = prune(next.Credential, time.Now())

	// Written unconditionally, even when mint changed nothing. Credentials holds a map
	// and two time.Time values, so a comparison is either uncompilable or subtly wrong
	// about monotonic clocks and locations — and pruning has to reach the file anyway.
	data, err := yaml.Marshal(next)
	if err != nil {
		return Credentials{}, diags.Wrap(err, "Cannot store the credentials",
			"%s could not be encoded.", s.path)
	}
	if err := writeFileAtomic(s.path, data); err != nil {
		return Credentials{}, errors.Join(err, ErrNotWritable)
	}
	return next, nil
}

func (s *fileStore) Forget() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return diags.Wrap(err, "Cannot remove the stored credentials",
			"%s could not be removed.", s.path)
	}
	return nil
}

// memoryStore is what a credential that did not come from a profile resolves to: an
// environment variable, a Terraform provider block, or a profile whose file cannot be
// written. Its token lives for the process and never lands in somebody's profile.
type memoryStore struct {
	mu    sync.Mutex
	creds Credentials
}

func NewMemoryStore(initial Credentials) Store {
	initial.Version = Version
	return &memoryStore{creds: initial}
}

func (s *memoryStore) Describe() string { return "in memory; nothing is written to disk" }

func (s *memoryStore) Writable() bool { return false }

func (s *memoryStore) Read() (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot(), nil
}

// Update needs no lock file and no re-read, because nothing outside this process can
// see these credentials. The mutex is only there because one process may renew from
// several goroutines.
func (s *memoryStore) Update(_ context.Context, mint func(Credentials) (Credentials, error)) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := mint(s.snapshot())
	if err != nil {
		return Credentials{}, err
	}
	next.Version = Version
	next.Credential = prune(next.Credential, time.Now())
	s.creds = next
	return next, nil
}

func (s *memoryStore) Forget() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds = Credentials{Version: Version}
	return nil
}

// snapshot deep-copies as far as a caller can reach, so that mutating what it read cannot
// change the store. A file store gets that for free.
func (s *memoryStore) snapshot() Credentials {
	creds := s.creds
	if creds.Login != nil {
		login := *creds.Login
		login.AccessTokens = maps.Clone(login.AccessTokens)
		creds.Login = &login
	}
	if creds.ApiKey != nil {
		apiKey := *creds.ApiKey
		creds.ApiKey = &apiKey
	}
	if creds.Manual != nil {
		manual := *creds.Manual
		creds.Manual = &manual
	}
	return creds
}
