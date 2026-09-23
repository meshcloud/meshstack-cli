package internal_test

import (
	"encoding/json/jsontext"
	"errors"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

// countingItems yields a, b, c, … and counts how many of them were asked for.
func countingItems(pulled *int) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
		for _, value := range []string{`"a"`, `"b"`, `"c"`, `"d"`} {
			*pulled++
			if !yield(jsontext.Value(value), nil) {
				return
			}
		}
	}
}

func collectValues(t *testing.T, items iter.Seq2[jsontext.Value, error]) []string {
	t.Helper()
	var got []string
	for item, err := range items {
		require.NoError(t, err)
		got = append(got, string(item))
	}
	return got
}

// A listing fetches its next page only when an item of it is asked for, so asking for no item past
// the limit is what keeps a small limit to a single request.
func TestFirstStopsAskingOnceItHasTheLimit(t *testing.T) {
	pulled := 0

	got := collectValues(t, internal.First(countingItems(&pulled), 2))

	assert.Equal(t, []string{`"a"`, `"b"`}, got)
	assert.Equal(t, 2, pulled)
}

func TestFirstWithoutALimitYieldsEveryItem(t *testing.T) {
	pulled := 0

	got := collectValues(t, internal.First(countingItems(&pulled), 0))

	assert.Equal(t, []string{`"a"`, `"b"`, `"c"`, `"d"`}, got)
}

func TestFirstPassesAnErrorOnAndStops(t *testing.T) {
	pageErr := errors.New("page 1 failed")
	failing := func(yield func(jsontext.Value, error) bool) {
		_ = yield(jsontext.Value(`"a"`), nil) && yield(nil, pageErr) && yield(jsontext.Value(`"b"`), nil)
	}

	var got []error
	for _, err := range internal.First(failing, 5) {
		got = append(got, err)
	}

	assert.Equal(t, []error{nil, pageErr}, got)
}

func TestTheLimitFlagTakesACountOfItems(t *testing.T) {
	for value, wantErr := range map[string]bool{"0": false, "50": false, "-1": true, "fifty": true} {
		t.Run(value, func(t *testing.T) {
			var limit internal.LimitFlag

			err := limit.Set(value)

			if wantErr {
				assert.ErrorContains(t, err, "is no limit")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCutShortNote(t *testing.T) {
	total := func(n int) *int { return &n }
	for name, tt := range map[string]struct {
		limit, listed int
		total         *int
		want          string
	}{
		"no limit":                        {limit: 0, listed: 1011, total: total(1011)},
		"fewer items than the limit":      {limit: 5000, listed: 1011, total: total(1011)},
		"as many items as the limit":      {limit: 50, listed: 50, total: total(50)},
		"more items than the limit":       {limit: 50, listed: 50, total: total(1011), want: "listed the first 50 of 1011; raise --limit, or set it to 0 to list all of them"},
		"no total to tell there are more": {limit: 50, listed: 50, want: "stopped at the limit of 50, there may be more; raise --limit, or set it to 0 to list all of them"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, internal.CutShortNote(tt.limit, tt.listed, tt.total))
		})
	}
}
