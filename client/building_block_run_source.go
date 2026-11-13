package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

type MeshBuildingBlockRunSourceRegistration struct {
	Source MeshBuildingBlockRunSource             `json:"source"`
	Steps  []MeshBuildingBlockRunStepRegistration `json:"steps"`
}

type MeshBuildingBlockRunSource struct {
	Id          string  `json:"id"`
	ExternalId  *string `json:"externalId,omitzero"`
	ExternalUrl *string `json:"externalUrl,omitzero"`
}

type MeshBuildingBlockRunStepRegistration struct {
	Id          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	Status      *string `json:"status"`
}

type MeshBuildingBlockRunStatusUpdate struct {
	BlockRunId      string                                             `json:"blockRunId,omitzero"`
	Source          string                                             `json:"source,omitzero"`
	Type            enum.Entry[MeshBuildingBlockRunImplementationType] `json:"type,omitempty"`
	Status          *string                                            `json:"status"`
	CreatedOn       time.Time                                          `json:"createdOn,omitzero"`
	Summary         *string                                            `json:"summary"`
	Steps           []MeshBuildingBlockRunStepStatusUpdate             `json:"steps"`
	ChangesDetected *bool                                              `json:"changesDetected,omitzero"`
}

type MeshBuildingBlockRunStepStatusUpdate struct {
	Id            string                                `json:"id"`
	DisplayName   string                                `json:"displayName"`
	Status        *string                               `json:"status,omitzero"`
	UserMessage   *string                               `json:"userMessage,omitzero"`
	SystemMessage *string                               `json:"systemMessage,omitzero"`
	Outputs       map[string]MeshBuildingBlockRunOutput `json:"outputs,omitempty"`
}

type MeshBuildingBlockRunOutput struct {
	Value     any    `json:"value"`
	Type      string `json:"type"`
	Sensitive bool   `json:"isSensitive"`
}

type MeshBuildingBlockRunStatusUpdateResult struct {
	Abort bool `json:"runAborted"`
}

const maxArtifactBytes = 128 << 20

func (c meshBuildingBlockRunClient) RegisterSource(ctx context.Context, runUuid string, registration MeshBuildingBlockRunSourceRegistration) error {
	err := http.IgnoreResponse(c.do(ctx, http.MethodPost, c.collection.JoinPath(runUuid, "status", "source"),
		http.WithJsonRequest(registration, c.mediaType), http.Retryable(http.RetryOnTransient)))
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsConflict() {
		return nil
	}
	return err
}

func (c meshBuildingBlockRunClient) UpdateSourceSteps(ctx context.Context, runUuid, sourceId string, update MeshBuildingBlockRunStatusUpdate) (MeshBuildingBlockRunStatusUpdateResult, error) {
	return http.UnmarshalJsonBody[MeshBuildingBlockRunStatusUpdateResult](c.do(ctx, http.MethodPatch, c.collection.JoinPath(runUuid, "status", "source", sourceId),
		http.WithJsonRequest(update, c.mediaType), http.Retryable(http.RetryOnTransient)))
}

func (c meshBuildingBlockRunClient) DownloadArtifact(ctx context.Context, runUuid string, w io.Writer) error {
	body, err := c.do(ctx, http.MethodGet, c.collection.JoinPath(runUuid, "artifact"), http.WithHeader("Accept", "application/octet-stream"))
	if err != nil {
		return err
	}
	_, err = io.Copy(&cappedWriter{w, maxArtifactBytes}, body)
	return errors.Join(err, body.Close())
}

func (c meshBuildingBlockRunClient) UploadArtifact(ctx context.Context, runUuid, path string) error {
	return http.IgnoreResponse(c.do(ctx, http.MethodPut, c.collection.JoinPath(runUuid, "artifact"),
		http.WithFileRequest(path, ""), http.WithHeader("Accept", "*/*")))
}

type cappedWriter struct {
	io.Writer

	remaining int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("the artifact exceeds %d bytes", maxArtifactBytes)
	}
	w.remaining -= int64(len(p))
	return w.Writer.Write(p)
}
