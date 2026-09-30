package prompt

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type candidate string

func (c candidate) Label() string { return "label of " + string(c) }

type labeled string

func (l labeled) Label() string { return string(l) }

func TestSelect(t *testing.T) {
	isB := func(c candidate) bool { return c == "b" }
	tests := []struct {
		name       string
		candidates []candidate
		isDefault  func(candidate) bool
		answers    string
		want       candidate
		wantErr    string
		wantAsked  string
	}{
		{
			name:    "no candidate is an error",
			wantErr: "no thing to select from",
		},
		{
			name:       "a single candidate is taken without asking",
			candidates: []candidate{"a"},
			want:       "a",
		},
		{
			name:       "an empty answer takes the default",
			candidates: []candidate{"a", "b"},
			isDefault:  isB,
			answers:    "\n",
			want:       "b",
			wantAsked:  "  [1] label of a\n *[2] label of b\nSelect a thing [1-2, default=2]: ",
		},
		{
			name:       "a number takes its candidate over the default",
			candidates: []candidate{"a", "b"},
			isDefault:  isB,
			answers:    "1\n",
			want:       "a",
			wantAsked:  "  [1] label of a\n *[2] label of b\nSelect a thing [1-2, default=2]: ",
		},
		{
			name:       "without a default an empty answer asks again",
			candidates: []candidate{"a", "b"},
			answers:    "\n0\n1\n",
			want:       "a",
			wantAsked: "  [1] label of a\n  [2] label of b\n" +
				"Select a thing [1-2]: Answer with a number between 1 and 2.\n" +
				"Select a thing [1-2]: Answer with a number between 1 and 2.\n" +
				"Select a thing [1-2]: ",
		},
		{
			name:       "a number out of range and letters ask again",
			candidates: []candidate{"a", "b"},
			isDefault:  isB,
			answers:    "3\nb\n1a\n2\n",
			want:       "b",
			wantAsked: "  [1] label of a\n *[2] label of b\n" +
				"Select a thing [1-2, default=2]: Answer with a number between 1 and 2.\n" +
				"Select a thing [1-2, default=2]: Answer with a number between 1 and 2.\n" +
				"Select a thing [1-2, default=2]: Answer with a number between 1 and 2.\n" +
				"Select a thing [1-2, default=2]: ",
		},
		{
			name:       "an input that ends without an answer is an error",
			candidates: []candidate{"a", "b"},
			wantErr:    "nothing was entered for the thing selection",
			wantAsked:  "  [1] label of a\n  [2] label of b\nSelect a thing [1-2]: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked bytes.Buffer

			selected, err := Select(t.Context(), New(strings.NewReader(tt.answers), &asked), "thing", tt.candidates, tt.isDefault)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, selected)
			assert.Equal(t, tt.wantAsked, asked.String())
		})
	}
}

func TestSelectPadsTheNumbers(t *testing.T) {
	var candidates []candidate
	var wantAsked strings.Builder
	for letter := 'a'; letter <= 'k'; letter++ {
		candidates = append(candidates, candidate(letter))
		fmt.Fprintf(&wantAsked, "  [%2d] label of %c\n", len(candidates), letter)
	}
	wantAsked.WriteString("Select a thing [1-11]: ")
	var asked bytes.Buffer

	selected, err := Select(t.Context(), New(strings.NewReader("10\n"), &asked), "thing", candidates, nil)

	require.NoError(t, err)
	assert.Equal(t, candidate("j"), selected)
	assert.Equal(t, wantAsked.String(), asked.String())
}

func TestSelectAlignsTheDetails(t *testing.T) {
	candidates := []labeled{"First (first)", "開発チーム (dev)", "No detail", "Second (second)"} //nolint:gosmopolitan // A wide label, to see it line up.
	var asked bytes.Buffer

	selected, err := Select(t.Context(), New(strings.NewReader("4\n"), &asked), "workspace", candidates, nil)

	require.NoError(t, err)
	assert.Equal(t, labeled("Second (second)"), selected)
	assert.Equal(t, "  [1] First      (first)\n"+
		"  [2] 開発チーム (dev)\n"+ //nolint:gosmopolitan // As above.
		"  [3] No detail\n"+
		"  [4] Second     (second)\n"+
		"Select a workspace [1-4]: ", asked.String())
}
