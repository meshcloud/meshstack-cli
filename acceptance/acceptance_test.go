// Package acceptance runs against a live local meshStack.
//
// Two things gate it, and both are deliberate:
//
//   - MESHSTACK_ACC=1, without which every test skips and says how to run it. It mirrors the
//     Terraform provider's TF_ACC, so one habit covers both repositories.
//   - MESHSTACK_ENDPOINT has to name a loopback address. These tests will log in and write
//     objects, and a stray export pointing them at a real meshStack is the accident worth making
//     impossible rather than merely unlikely.
package acceptance

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	envAcc      = "MESHSTACK_ACC"
	envEndpoint = "MESHSTACK_ENDPOINT"

	acceptanceEnabled = "1"

	loopbackHost        = "http://localhost"
	loopbackAddressHost = "http://127.0.0.1"
)

// reachabilityTimeout bounds the one request: generous for a loopback GET of a public document,
// short enough that a stack which is down is reported rather than waited on.
const reachabilityTimeout = 10 * time.Second

func TestAccMeshInfo(t *testing.T) {
	if os.Getenv(envAcc) != acceptanceEnabled {
		t.Skipf("acceptance tests are off. Bring up a local dev stack and run `%s=%s %s=%s:8080 go test ./acceptance/... -run TestAcc`",
			envAcc, acceptanceEnabled, envEndpoint, loopbackHost)
	}

	endpoint := strings.TrimSuffix(os.Getenv(envEndpoint), "/")
	if !strings.HasPrefix(endpoint, loopbackHost) && !strings.HasPrefix(endpoint, loopbackAddressHost) {
		t.Fatalf("%s=%q does not name a loopback address, so this suite refuses to run against it.",
			envEndpoint, os.Getenv(envEndpoint))
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+"/mesh/info", nil)
	if err != nil {
		t.Fatalf("cannot build the request: %v", err)
	}

	// Its own client, because the timeout belongs to this request alone and http.DefaultClient is
	// shared with whatever else ends up using it.
	resp, err := (&http.Client{Timeout: reachabilityTimeout}).Do(req)
	if err != nil {
		t.Fatalf("the backend at %s is not reachable, so nothing in this package can run. Bring the local dev stack up first: %v", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("%s/mesh/info answered %s", endpoint, resp.Status)
	}
}
