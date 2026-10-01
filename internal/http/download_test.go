package http_test

import (
	"compress/gzip"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

type fakeDocument struct {
	body     string
	modified time.Time
	missing  bool
	release  chan struct{}
	answers  []int
	// contentEncodings has one entry per document sent, and none per 304 or 404.
	contentEncodings []string
}

func (d *fakeDocument) serve(resp gohttp.ResponseWriter, req *gohttp.Request) {
	if d.release != nil {
		<-d.release
	}
	since, sinceErr := gohttp.ParseTime(req.Header.Get("If-Modified-Since"))
	switch {
	case d.missing:
		d.answers = append(d.answers, gohttp.StatusNotFound)
		resp.WriteHeader(gohttp.StatusNotFound)
	case sinceErr == nil && !since.Before(d.modified):
		d.answers = append(d.answers, gohttp.StatusNotModified)
		resp.WriteHeader(gohttp.StatusNotModified)
	default:
		d.answers = append(d.answers, gohttp.StatusOK)
		resp.Header().Set("Last-Modified", d.modified.Format(gohttp.TimeFormat))
		if !strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
			d.contentEncodings = append(d.contentEncodings, "identity")
			_, _ = resp.Write([]byte(d.body))
			return
		}
		d.contentEncodings = append(d.contentEncodings, "gzip")
		resp.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(resp)
		_, _ = compressed.Write([]byte(d.body))
		_ = compressed.Close()
	}
}

func TestDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	document := &fakeDocument{body: `{"version": 1}`, modified: time.Date(2026, 9, 30, 13, 49, 36, 0, time.UTC)}
	download := func(t *testing.T, options ...http.DownloadOption) (wait func()) {
		t.Helper()
		client := newTestClientWithServer(t, document.serve)
		wait, err := client.Download(t.Context(), client.ServerUrl, path, options...)
		require.NoError(t, err)
		return wait
	}

	t.Run("stores the document, and checks once a day for a newer one, even after a failed check", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			testLogger := installTestLogger(t)

			download(t)()
			assert.Equal(t, []string{"gzip"}, document.contentEncodings, "the 4.7MB of the API docs come as 0.5MB")
			assert.JSONEq(t, `{"version": 1}`, readDocument(t, path), "the stored document is decompressed")
			stat, err := os.Stat(path)
			require.NoError(t, err)
			assert.True(t, document.modified.Equal(stat.ModTime()), "the mtime is the Last-Modified")

			synctest.Sleep(23 * time.Hour)
			download(t)()
			assert.Equal(t, []int{gohttp.StatusOK}, document.answers, "a check within a day is not due")

			synctest.Sleep(time.Hour)
			download(t)()
			assert.Equal(t, []int{gohttp.StatusOK, gohttp.StatusNotModified}, document.answers)
			assert.Empty(t, testLogger.Warns)

			synctest.Sleep(24 * time.Hour)
			document.missing = true
			download(t)()
			download(t)()
			assert.Equal(t, []int{gohttp.StatusOK, gohttp.StatusNotModified, gohttp.StatusNotFound}, document.answers)
			require.Len(t, testLogger.Warns, 1)
			assert.Contains(t, testLogger.Warns[0], "Cannot update "+path)
			assert.JSONEq(t, `{"version": 1}`, readDocument(t, path))
		})
	})

	t.Run("goes on with the stored document while a slow download finishes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			testLogger := installTestLogger(t)
			document.missing = false
			document.body, document.modified = `{"version": 2}`, document.modified.Add(time.Hour)
			document.release = make(chan struct{})

			wait := download(t, http.WithConditionalGet(true))
			assert.JSONEq(t, `{"version": 1}`, readDocument(t, path), "the caller reads the stored document")
			require.Len(t, testLogger.Warns, 1)
			assert.Contains(t, testLogger.Warns[0], "The command ends once the download has finished.")

			close(document.release)
			wait()
			assert.JSONEq(t, `{"version": 2}`, readDocument(t, path), "the next command reads the new document")
		})
	})

	t.Run("fails without a stored document to fall back to, and leaves nothing behind", func(t *testing.T) {
		client := newTestClientWithServer(t, (&fakeDocument{missing: true}).serve)
		dir := t.TempDir()

		_, err := client.Download(t.Context(), client.ServerUrl, filepath.Join(dir, "doc.json"))

		require.ErrorContains(t, err, "404")
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}

func readDocument(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	return string(content)
}
