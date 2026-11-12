package http

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_ShowsTheBodyAsItCame(t *testing.T) {
	err := Error{Method: MethodPost, Url: "https://id.example.com/token", StatusCode: 401, ResponseBody: []byte(`{"error":"invalid_client"}` + "\n")}

	assert.Equal(t, `POST https://id.example.com/token: HTTP 401: {"error":"invalid_client"}`, err.Error())
	assert.Equal(t, "HTTP 502", Error{StatusCode: 502}.Error())
	body, bodyErr := err.BodyAsJson[struct {
		Error string `json:"error"`
	}]()
	require.NoError(t, bodyErr)
	assert.Equal(t, "invalid_client", body.Error)
}
