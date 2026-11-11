package http

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorIsReadFromTheFieldsOfTheBody(t *testing.T) {
	t.Run("shows the clientVersionHint on a line of its own", func(t *testing.T) {
		err := Error{StatusCode: 404, ResponseBody: []byte(
			`{"errorCode":"NoHandlerFound","message":"not found","errorId":"abc","clientVersionHint":"Upgrade to v0.9.0."}`)}

		assert.Equal(t, "Upgrade to v0.9.0.", err.ClientVersionHint())
		assert.Equal(t, "NoHandlerFound", err.ErrorCode())
		assert.Equal(t, "meshStack answered HTTP 404: not found (errorCode NoHandlerFound, errorId abc)\nUpgrade to v0.9.0.", err.Error())
	})

	t.Run("ignores a field it does not know", func(t *testing.T) {
		err := Error{StatusCode: 409, ResponseBody: []byte(`{"errorCode":"X","message":"m","other":1}`)}

		assert.Equal(t, "meshStack answered HTTP 409: m (errorCode X)", err.Error())
	})

	t.Run("shows no hint where there is none", func(t *testing.T) {
		err := Error{StatusCode: 422, ResponseBody: []byte(
			`{"errorCode":"Invalid","message":"bad","errorId":"x1"}`)}

		assert.Empty(t, err.ClientVersionHint())
		assert.Equal(t, "meshStack answered HTTP 422: bad (errorCode Invalid, errorId x1)", err.Error())
	})

	for name, body := range map[string]string{
		"no JSON":         `<html>Bad Gateway</html>`,
		"no body":         ``,
		"an empty object": `{}`,
	} {
		t.Run("shows the raw body for "+name, func(t *testing.T) {
			err := Error{StatusCode: 502, ResponseBody: []byte(body)}

			assert.Empty(t, err.ClientVersionHint())
			assert.Equal(t, "meshStack answered HTTP 502, but the response is not in the expected error format: '"+body+"'", err.Error())
			assert.Equal(t, "meshStack answered HTTP 502", err.Summary())
		})
	}
}
