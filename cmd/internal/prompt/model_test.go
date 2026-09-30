package prompt

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel(t *testing.T) {
	var (
		digit     = func(d rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: d, Text: string(d)} }
		enter     = tea.KeyPressMsg{Code: tea.KeyEnter}
		escape    = tea.KeyPressMsg{Code: tea.KeyEscape}
		backspace = tea.KeyPressMsg{Code: tea.KeyBackspace}
		ctrlC     = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	)
	tests := []struct {
		name         string
		keys         []tea.KeyPressMsg
		wantSelected int
		wantAborted  bool
		wantNumber   string
		wantProblem  string
	}{
		{name: "digits and Enter take that number", keys: []tea.KeyPressMsg{digit('1'), digit('2'), enter}, wantSelected: 12},
		{name: "Enter takes the highlighted default", keys: []tea.KeyPressMsg{enter}, wantSelected: 3},
		{
			name:        "a number out of range shows what to answer",
			keys:        []tea.KeyPressMsg{digit('1'), digit('3'), enter},
			wantProblem: "Answer with a number between 1 and 12.",
		},
		{name: "Esc aborts", keys: []tea.KeyPressMsg{escape}, wantAborted: true},
		{name: "Ctrl-C aborts", keys: []tea.KeyPressMsg{digit('1'), ctrlC}, wantAborted: true, wantNumber: "1"},
		{name: "Esc clears a typed number rather than abort", keys: []tea.KeyPressMsg{digit('1'), digit('2'), escape}},
		{name: "Backspace edits the number", keys: []tea.KeyPressMsg{digit('1'), digit('2'), backspace}, wantNumber: "1"},
		{name: "Backspace and Enter take the edited number", keys: []tea.KeyPressMsg{digit('1'), digit('2'), backspace, digit('1'), enter}, wantSelected: 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := make([]item, 0, 12)
			for number := 1; number <= 12; number++ {
				items = append(items, newItem(number, "candidate", number == 3))
			}
			m := newModel("thing", items, 3)

			for _, key := range tt.keys {
				m, _ = m.update(key)
			}

			assert.Equal(t, tt.wantSelected, m.selected.number)
			assert.Equal(t, tt.wantAborted, m.aborted)
			assert.Equal(t, tt.wantSelected != 0 || tt.wantAborted, m.done)
			if tt.wantSelected == 0 {
				assert.Equal(t, tt.wantNumber, m.number)
			}
			assert.Equal(t, tt.wantProblem, m.problem)
			if tt.wantProblem != "" {
				require.Contains(t, m.View().Content, tt.wantProblem)
			}
		})
	}
}

// The inline renderer loses track of the cursor when a frame shrinks, see model.View.
func TestModelViewKeepsItsSize(t *testing.T) {
	var (
		key   = func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
		enter = tea.KeyPressMsg{Code: tea.KeyEnter}
		esc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	)
	// Every character on the frame takes one cell.
	width := func(line string) int {
		return utf8.RuneCountInString(regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(line, ""))
	}
	long := strings.Repeat("a-rather-long-name-", 5)
	items := []item{newItem(1, "dev", false), newItem(2, long+"(with-an-identifier)", true), newItem(3, "staging", false)}
	const terminalWidth = 50
	m := newModel("profile", items, 2)
	m, _ = m.update(tea.WindowSizeMsg{Width: terminalWidth, Height: 30})
	frameHeight := strings.Count(m.View().Content, "\n") + 1

	for _, keys := range [][]tea.KeyPressMsg{
		{key('1')},
		{esc},
		{key('9'), enter},
		{key('?')},
		{key('/'), key('s')},
		{esc},
		{key('3'), enter},
	} {
		for _, k := range keys {
			m, _ = m.update(k)
		}
		lines := strings.Split(m.View().Content, "\n")
		assert.Len(t, lines, frameHeight)
		for _, line := range lines {
			assert.LessOrEqual(t, width(line), terminalWidth, line)
			if !strings.Contains(line, long[:10]) {
				assert.NotContains(t, line, "…", "only the long entry is cut off")
			}
		}
	}
	require.True(t, m.done)
	assert.Equal(t, "Selected profile: staging", m.outcome())
}

func TestModelFitsAShortTerminal(t *testing.T) {
	items := make([]item, 0, 12)
	for number := 1; number <= 12; number++ {
		items = append(items, newItem(number, "candidate", false))
	}
	m := newModel("thing", items, 0)
	m, _ = m.update(tea.WindowSizeMsg{Width: 80, Height: 8})

	assert.Equal(t, 8, strings.Count(m.View().Content, "\n")+1)
}

func TestModelCountsTheCandidatesPastThePage(t *testing.T) {
	items := make([]item, 0, 12)
	for number := 1; number <= 12; number++ {
		items = append(items, newItem(number, "candidate", false))
	}
	m := newModel("thing", items, 0)
	m, _ = m.update(tea.WindowSizeMsg{Width: 80, Height: 30})
	frameHeight := strings.Count(m.View().Content, "\n") + 1
	assert.Contains(t, m.View().Content, "… 2 more")

	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.NotContains(t, m.View().Content, "more")
	assert.Equal(t, frameHeight, strings.Count(m.View().Content, "\n")+1)
}
