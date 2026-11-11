package apidocs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

type fallbackError struct {
	error
}

type store struct {
	dir string
}

type endpointIndex struct {
	Endpoint string `json:"endpoint"`
	Document string `json:"document"`
	ETag     string `json:"etag,omitzero"`
}

func (s store) indexDir() string {
	return filepath.Join(s.dir, "endpoints")
}

func (s store) indexPath(endpoint xurl.URL) string {
	hash := sha256.Sum256([]byte(endpoint.String()))
	return filepath.Join(s.indexDir(), hex.EncodeToString(hash[:8])+".json")
}

func (s store) documentPath(hash string) string {
	return filepath.Join(s.dir, hash+".json")
}

func (s store) load(ctx context.Context, client http.Client, endpoint xurl.URL) (openapi.Spec, error) {
	url := xurl.URL{URL: endpoint.JoinPath("api", "openapi")}
	indexPath := s.indexPath(endpoint)
	stored, storedDocument := s.read(ctx, indexPath)
	var options []http.RequestOption
	if storedDocument != nil && stored.ETag != "" {
		options = append(options, http.WithHeader("If-None-Match", stored.ETag))
	}

	document, header, err := download(ctx, client, url, options...)
	httpErr, isHttpErr := errors.AsType[http.Error](err)
	switch {
	case isHttpErr && httpErr.StatusCode == gohttp.StatusNotModified:
		slog.DebugContext(ctx, fmt.Sprintf("The API docs of %s are up to date", endpoint))
		return parse(url, storedDocument)
	// A meshStack from before /api/openapi answers 401.
	case isHttpErr && (httpErr.StatusCode == gohttp.StatusUnauthorized || httpErr.StatusCode == gohttp.StatusNotFound):
		return openapi.Spec{}, fallbackError{fmt.Errorf("%s serves no API docs", endpoint)}
	case err != nil && storedDocument != nil:
		slog.WarnContext(ctx, fmt.Sprintf("Cannot load the API docs from %s, so this command reads the ones stored before: %s", url, err))
		return parse(url, storedDocument)
	case err != nil:
		return openapi.Spec{}, fallbackError{fmt.Errorf("cannot load the API docs from %s: %w", url, err)}
	}

	spec, err := parse(url, document)
	if err != nil {
		return spec, err
	}
	hash := sha256.Sum256(document)
	index := endpointIndex{Endpoint: endpoint.String(), Document: hex.EncodeToString(hash[:]), ETag: header.Get("ETag")}
	if storeErr := s.put(ctx, indexPath, index, document, stored.Document); storeErr != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cannot store the API docs of %s: %s", endpoint, storeErr))
	}
	return spec, nil
}

func (s store) read(ctx context.Context, indexPath string) (endpointIndex, []byte) {
	var index endpointIndex
	if err := json.UnmarshalFrom(ctx, indexPath, &index); err != nil {
		return endpointIndex{}, nil
	}
	document, err := os.ReadFile(s.documentPath(index.Document))
	if hash := sha256.Sum256(document); err != nil || hex.EncodeToString(hash[:]) != index.Document {
		return index, nil
	}
	return index, document
}

func (s store) put(ctx context.Context, indexPath string, index endpointIndex, document []byte, previous string) error {
	if err := writeFile(s.documentPath(index.Document), document); err != nil {
		return err
	}
	if err := json.MarshalTo(ctx, indexPath, index); err != nil {
		return err
	}
	if previous == "" || previous == index.Document {
		return nil
	}
	return s.removeUnreferenced(ctx)
}

func (s store) removeUnreferenced(ctx context.Context) error {
	indexes, err := filepath.Glob(filepath.Join(s.indexDir(), "*.json"))
	if err != nil {
		return err
	}
	referenced := map[string]bool{}
	for _, path := range indexes {
		var index endpointIndex
		if readErr := json.UnmarshalFrom(ctx, path, &index); readErr != nil {
			return readErr
		}
		referenced[index.Document] = true
	}
	documents, err := filepath.Glob(s.documentPath("*"))
	if err != nil {
		return err
	}
	var errs []error
	for _, path := range documents {
		if !referenced[strings.TrimSuffix(filepath.Base(path), ".json")] {
			errs = append(errs, os.Remove(path))
		}
	}
	return errors.Join(errs...)
}

func writeFile(path string, content []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	_, writeErr := tmp.Write(content)
	if err = errors.Join(writeErr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
