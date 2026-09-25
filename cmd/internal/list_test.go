package internal_test

import (
	"encoding/json/jsontext"
	"errors"
	"iter"
	"testing"

	"github.com/spf13/cobra"
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
func TestFirstYieldsTheFirstNAndStopsAsking(t *testing.T) {
	for name, tt := range map[string]struct {
		limit, wantPulled int
		want              []string
	}{
		"limit":    {limit: 2, wantPulled: 2, want: []string{`"a"`, `"b"`}},
		"no limit": {limit: 0, wantPulled: 4, want: []string{`"a"`, `"b"`, `"c"`, `"d"`}},
	} {
		t.Run(name, func(t *testing.T) {
			pulled := 0

			got := collectValues(t, internal.First(countingItems(&pulled), tt.limit))

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantPulled, pulled)
		})
	}
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

func TestTheLimitFlagTakesACountOfItemsOrAll(t *testing.T) {
	for value, wantErr := range map[string]bool{"50": false, "unlimited": false, "0": true, "-1": true, "fifty": true, "all": true} {
		t.Run(value, func(t *testing.T) {
			var limit internal.LimitFlag

			err := limit.Set(value)

			if wantErr {
				assert.ErrorContains(t, err, "is no limit")
			} else {
				require.NoError(t, err)
				assert.Equal(t, value, limit.String())
			}
		})
	}
}

func TestAListingIsLimitedByDefault(t *testing.T) {
	cmd := &cobra.Command{}
	var listFlags internal.ListFlags

	listFlags.Register(cmd.Flags())

	assert.Equal(t, "100", cmd.Flags().Lookup("limit").DefValue)
}

func TestCutShortNote(t *testing.T) {
	for name, tt := range map[string]struct {
		limit, listed int
		total         *int
		defaulted     bool
		want          string
	}{
		"no limit":                                   {limit: 0, listed: 1011, total: new(1011)},
		"fewer items than the limit":                 {limit: 5000, listed: 1011, total: new(1011)},
		"as many items as the limit":                 {limit: 50, listed: 50, total: new(50)},
		"more items than the limit":                  {limit: 50, listed: 50, total: new(1011), want: "listed the first 50 of 1011; raise --limit, or pass --limit unlimited to list all of them"},
		"no total to tell there are more":            {limit: 50, listed: 50, want: "stopped at the limit of 50, there may be more; raise --limit, or pass --limit unlimited to list all of them"},
		"more items than the default limit":          {limit: 100, listed: 100, total: new(1011), defaulted: true, want: "listed the first 100 of 1011, the default limit; raise --limit, or pass --limit unlimited to list all of them"},
		"no total to tell there are more by default": {limit: 100, listed: 100, defaulted: true, want: "stopped at the default limit of 100, there may be more; raise --limit, or pass --limit unlimited to list all of them"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, internal.CutShortNote(tt.limit, tt.listed, tt.total, tt.defaulted))
		})
	}
}
