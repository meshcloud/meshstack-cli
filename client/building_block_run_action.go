package client

import (
	"context"
	"fmt"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

// Link is a HAL link. meshStack sends the link of an action only while the caller may take it.
type Link struct {
	Href string `json:"href"`
}

type MeshBuildingBlockRunLinks struct {
	Predecessor  *Link `json:"predecessor"`
	DownloadLogs *Link `json:"downloadLogs"`
	Approve      *Link `json:"approve"`
	Abort        *Link `json:"abort"`
}

// MeshBuildingBlockV2RunLinks are not a field of MeshBuildingBlockV2, because the Terraform provider
// maps every field of that type to its schema.
type MeshBuildingBlockV2RunLinks struct {
	LatestRun    *Link `json:"latestRun"`
	LatestDryRun *Link `json:"latestDryRun"`
}

type MeshBuildingBlockRunActionClient struct {
	httpClient internal.HttpClient
	run        internal.MeshObjectApi
}

func newBuildingBlockRunActionClient(httpClient internal.HttpClient, run meshBuildingBlockRunClient) *MeshBuildingBlockRunActionClient {
	return &MeshBuildingBlockRunActionClient{httpClient: httpClient, run: run.meshObject.MeshObjectApi}
}

func (c *MeshBuildingBlockRunActionClient) ReadRun(ctx context.Context, link Link) (MeshBuildingBlockRun, error) {
	return c.doAtLink[MeshBuildingBlockRun](ctx, http.MethodGet, link)
}

func (c *MeshBuildingBlockRunActionClient) ReadLogs(ctx context.Context, link Link) (MeshBuildingBlockRunLogs, error) {
	return c.doAtLink[MeshBuildingBlockRunLogs](ctx, http.MethodGet, link)
}

// ApproveRun takes the predecessor because meshStack replaces the plan of a run that waits for
// approval when the run is planned again. The predecessor names the plan that is approved.
func (c *MeshBuildingBlockRunActionClient) ApproveRun(ctx context.Context, approve Link, predecessorRunUuid string) error {
	_, err := c.doAtLink[[]byte](ctx, http.MethodPost, approve, http.WithJsonPayload(struct {
		PredecessorRunUuid string `json:"predecessorRunUuid"`
	}{predecessorRunUuid}, c.run.MeshObjectMimeType()))
	return err
}

func (c *MeshBuildingBlockRunActionClient) AbortRun(ctx context.Context, abort Link) error {
	_, err := c.doAtLink[[]byte](ctx, http.MethodPost, abort)
	return err
}

// doAtLink sends the request only below the endpoint, so that the bearer token never leaves for
// another host that a link names.
func (c *MeshBuildingBlockRunActionClient) doAtLink[R any](ctx context.Context, method string, link Link, options ...http.RequestOption) (R, error) {
	var noResult R
	href, err := url.Parse(link.Href)
	if err != nil {
		return noResult, fmt.Errorf("meshStack sent the link %q, which is no URL: %w", link.Href, err)
	}
	target := c.httpClient.EndpointUrl.ResolveReference(href)
	path, isBelowEndpoint := c.httpClient.EndpointUrl.PathTo(target)
	if !isBelowEndpoint {
		return noResult, fmt.Errorf("meshStack sent the link %s, which is not below the endpoint %s", target.Redacted(), c.httpClient.EndpointUrl)
	}
	below := c.httpClient.EndpointUrl.JoinPath(path)
	below.RawQuery = target.RawQuery
	return c.httpClient.DoRequest[R](ctx, method, below, append(options, http.WithAccept(c.run.MeshObjectMimeType()))...)
}
