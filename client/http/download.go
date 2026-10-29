package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	gohttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

const (
	// A command starts without an open connection, and DNS, TCP, TLS and a 304 from CloudFront took
	// 55 to 100ms, so a shorter wait would give up on a fast network as well.
	waitForNewerDownload  = 200 * time.Millisecond
	downloadTimeout       = 2 * time.Minute
	downloadCheckInterval = 24 * time.Hour
)

type (
	DownloadOption func(opts *downloadOptions)

	downloadOptions struct {
		conditionalGet bool
	}
)

// WithConditionalGet asks for a newer document on every download. Without it, Download asks
// once per downloadCheckInterval, to save the round trip.
func WithConditionalGet(conditionalGet bool) DownloadOption {
	return func(opts *downloadOptions) {
		opts.conditionalGet = conditionalGet
	}
}

// Download waits at most waitForNewerDownload for a newer document where path holds one already.
// After that, the caller reads the stored document, and wait lets the download finish for the next
// command.
func (c Client) Download(ctx context.Context, url *url.URL, path string, options ...DownloadOption) (wait func(), err error) {
	var opts downloadOptions
	for _, option := range options {
		option(&opts)
	}
	noWait := func() {}
	stored, statErr := os.Stat(path)
	if statErr != nil {
		downloadCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()
		if err := c.download(downloadCtx, url, path, nil); err != nil {
			return noWait, err
		}
		return noWait, opts.recordCheck(ctx, path)
	}
	if due, err := opts.checkDue(ctx, path); err != nil || !due {
		return noWait, err
	}

	done := make(chan error, 1)
	go func() {
		// Derived from ctx, so that Ctrl-C ends the wait: until the command returns, main's
		// signal.NotifyContext swallows every further Ctrl-C.
		downloadCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()
		err := c.download(downloadCtx, url, path, []RequestOption{
			withHeader("If-Modified-Since", stored.ModTime().UTC().Format(gohttp.TimeFormat)),
		})
		// A cancelled download leaves the check due, so that the next command tries again.
		if ctx.Err() != nil {
			done <- nil
			return
		}
		done <- errors.Join(err, opts.recordCheck(ctx, path))
	}()
	warnIfFailed := func(err error) {
		if err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("Cannot update %s, which is from %s: %s", path, stored.ModTime().Format(time.DateTime), err))
		}
	}
	select {
	case err := <-done:
		warnIfFailed(err)
		return noWait, nil
	case <-time.After(waitForNewerDownload):
		slog.WarnContext(ctx, fmt.Sprintf("Cannot update %s within %s, so this command reads the one from %s. "+
			"The command ends once the download has finished.", path, waitForNewerDownload, stored.ModTime().Format(time.DateTime)))
		return func() { warnIfFailed(<-done) }, nil
	}
}

type downloadCheck struct {
	LastCheck time.Time `json:"lastCheck"`
}

func lastCheckJson(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".lastcheck.json"
}

func (opts downloadOptions) checkDue(ctx context.Context, path string) (bool, error) {
	if opts.conditionalGet {
		return true, nil
	}
	var check downloadCheck
	if err := json.UnmarshalFrom(ctx, lastCheckJson(path), &check); errors.Is(err, fs.ErrNotExist) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	if untilNext := downloadCheckInterval - time.Since(check.LastCheck); untilNext > 0 {
		slog.DebugContext(ctx, fmt.Sprintf("Not checking %s for a newer one, the next check is due in %s", path, untilNext.Round(time.Minute)))
		return false, nil
	}
	return true, nil
}

func (opts downloadOptions) recordCheck(ctx context.Context, path string) error {
	if opts.conditionalGet {
		return nil
	}
	return json.MarshalTo(ctx, lastCheckJson(path), downloadCheck{time.Now()})
}

func (c Client) download(ctx context.Context, url *url.URL, path string, options []RequestOption) (err error) {
	// No option sets an Accept-Encoding, so that net/http asks for gzip and decompresses the answer:
	// the API docs then come as 0.5MB rather than 4.7MB. CloudFront serves brotli as well, but it
	// saves only 4 to 8% over gzip, too little for a dependency.
	res, err := c.send(ctx, gohttp.MethodGet, url, options)
	if err != nil {
		return err
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode == gohttp.StatusNotModified {
		slog.DebugContext(ctx, path+" is up to date")
		return nil
	}
	if !isSuccess(res.StatusCode) {
		_, err = c.readBodyAndCheckSuccess(ctx, res)
		return err
	}

	// The document is renamed into place, so that no reader sees half of it.
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.Remove(tmp.Name()); !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	_, copyErr := io.Copy(tmp, res.Body)
	if err = errors.Join(copyErr, tmp.Close()); err != nil {
		return fmt.Errorf("cannot download %s: %w", url, err)
	}
	if lastModified, parseErr := gohttp.ParseTime(res.Header.Get("Last-Modified")); parseErr == nil {
		if err = os.Chtimes(tmp.Name(), time.Time{}, lastModified); err != nil {
			return err
		}
	}
	slog.DebugContext(ctx, fmt.Sprintf("downloaded %s to %s", url, path))
	return os.Rename(tmp.Name(), path)
}
