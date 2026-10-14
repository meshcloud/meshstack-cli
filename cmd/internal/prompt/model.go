package prompt

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/meshcloud/meshstack-cli/internal/logs"
)

type item struct {
	number    int
	name      string
	detail    string
	isDefault bool
}

// FilterValue matches what the entry shows, not the styling of a name rendered for the terminal.
func (i item) FilterValue() string { return ansi.Strip(i.label()) }

func (i item) label() string { return i.name + i.detail }

// delegate counts widths in terminal cells rather than bytes, so that wide characters such as CJK
// or emoji line up as well.
type delegate struct{ numberWidth, nameWidth int }

func newDelegate(items []item) delegate {
	d := delegate{numberWidth: len(strconv.Itoa(len(items)))}
	for _, candidate := range items {
		d.nameWidth = max(d.nameWidth, ansi.StringWidth(candidate.name))
	}
	return d
}

func (delegate) Height() int                         { return 1 }
func (delegate) Spacing() int                        { return 0 }
func (delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d delegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	candidate, _ := listItem.(item)
	cursor := "  "
	if index == m.Index() {
		cursor = "> "
	}
	// The list pads every line to the widest one, so a long entry would cut off all of them.
	_, _ = io.WriteString(w, ansi.Truncate(cursor+d.entry(candidate), m.Width(), "…"))
}

func (d delegate) entry(candidate item) string {
	defaultMarker, padding := " ", ""
	if candidate.isDefault {
		defaultMarker = "*"
	}
	if candidate.detail != "" {
		padding = strings.Repeat(" ", d.nameWidth-ansi.StringWidth(candidate.name))
	}
	return fmt.Sprintf("%s[%*d] %s%s%s", defaultMarker, d.numberWidth, candidate.number, candidate.name, padding, candidate.detail)
}

// model is the selection both drivers share: the terminal UI sends it the keys pressed, and the
// line driver the keys an answer stands for.
type model struct {
	list          list.Model
	delegate      delegate
	what          string
	defaultNumber int
	number        string
	problem       string
	width         int
	warned        logs.Warned
	selected      item
	aborted       bool
	done          bool
}

func newModel(what string, items []item, defaultNumber int) model {
	listItems := make([]list.Item, 0, len(items))
	for _, candidate := range items {
		listItems = append(listItems, candidate)
	}
	d := newDelegate(items)
	candidateList := list.New(listItems, d, 80, listHeight(len(items)))
	candidateList.Title = "Select a " + what
	candidateList.Styles.TitleBar = candidateList.Styles.TitleBar.UnsetPadding()
	candidateList.Styles.Title = candidateList.Styles.Title.UnsetBackground().UnsetForeground().UnsetPadding().Bold(true)
	candidateList.Styles.HelpStyle = candidateList.Styles.HelpStyle.UnsetPaddingTop()
	candidateList.SetShowStatusBar(false)
	// View renders the help itself, below a line that counts the candidates past the page.
	candidateList.SetShowPagination(false)
	candidateList.SetShowHelp(false)
	candidateList.DisableQuitKeybindings()
	candidateList.Filter = substringFilter
	// The full help takes more lines than the list has, and the list would re-enable a merely disabled binding.
	candidateList.KeyMap.ShowFullHelp.SetKeys()
	candidateList.KeyMap.CloseFullHelp.SetKeys()
	candidateList.Select(max(defaultNumber-1, 0))
	return model{list: candidateList, delegate: d, what: what, defaultNumber: defaultNumber}
}

// substringFilter replaces the list's default fuzzy filter, which matches the letters of term one
// by one anywhere in a label. With it, a short term matches nearly every label with an endpoint.
func substringFilter(term string, targets []string) []list.Rank {
	needle := []rune(strings.ToLower(term))
	var ranks []list.Rank
	for index, target := range targets {
		haystack := []rune(strings.ToLower(target))
		for start := 0; start+len(needle) <= len(haystack); start++ {
			if slices.Equal(haystack[start:start+len(needle)], needle) {
				matched := make([]int, len(needle))
				for i := range matched {
					matched[i] = start + i
				}
				ranks = append(ranks, list.Rank{Index: index, MatchedIndexes: matched})
				break
			}
		}
	}
	return ranks
}

// linesBelowList are the lines View puts below the list: the "… more" line, the help and the status.
const linesBelowList = 3

// listHeight leaves room for the title.
func listHeight(candidates int) int {
	const visibleCandidates = 10
	return min(candidates, visibleCandidates) + 1
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.update(msg)
}

// View keeps its height from the first frame to the last, including the one the program ends on:
// the inline renderer of bubbletea v2.0.10 clamps its cursor position to a shrinking frame and then
// redraws it from the wrong line. selectOnTerminal replaces the last frame with the outcome instead.
func (m model) View() tea.View {
	status := cmp.Or(m.problem, m.warned.String())
	if m.number != "" {
		status = fmt.Sprintf("Number: %s (Enter takes it, Esc clears it)", m.number)
	}
	more := ""
	if _, end := m.list.Paginator.GetSliceBounds(len(m.list.VisibleItems())); end < len(m.list.VisibleItems()) {
		more = m.list.Styles.PaginationStyle.Render(fmt.Sprintf("… %d more", len(m.list.VisibleItems())-end))
	}
	help := m.list.Styles.HelpStyle.Render(m.list.Help.View(m.list))
	frame := strings.Join([]string{m.list.View(), more, help, status}, "\n")
	if m.width > 0 {
		// A line the terminal wraps takes more rows than the renderer counts.
		lines := strings.Split(frame, "\n")
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, m.width, "…")
		}
		frame = strings.Join(lines, "\n")
	}
	return tea.NewView(frame)
}

func (m model) outcome() string {
	if m.aborted {
		return fmt.Sprintf("No %s selected.", m.what)
	}
	return fmt.Sprintf("Selected %s: %s", m.what, m.selected.label())
}

// update is Update keeping the model's type, for the line driver and the tests.
func (m model) update(msg tea.Msg) (model, tea.Cmd) {
	switch msg := msg.(type) {
	case logs.Warned:
		m.warned = msg
		return m, nil
	case tea.WindowSizeMsg:
		// The frame has to fit on the terminal, or the renderer drops lines from its top.
		m.width = msg.Width
		m.list.SetSize(msg.Width, min(listHeight(len(m.list.Items())), msg.Height-linesBelowList))
	case tea.KeyPressMsg:
		m.problem = ""
		filtering := m.list.FilterState() == list.Filtering
		switch keyName := msg.String(); {
		case keyName == "ctrl+c", keyName == "esc" && m.list.FilterState() == list.Unfiltered && m.number == "":
			m.aborted, m.done = true, true
			return m, tea.Quit
		case keyName == "enter":
			return m.selectOnEnter()
		case !filtering && len(keyName) == 1 && keyName >= "0" && keyName <= "9":
			m.number += keyName
			return m, nil
		case !filtering && keyName == "backspace" && m.number != "":
			m.number = m.number[:len(m.number)-1]
			return m, nil
		case !filtering && keyName == "esc" && m.number != "":
			m.number = ""
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) selectOnEnter() (model, tea.Cmd) {
	if m.number != "" {
		number, err := strconv.Atoi(m.number)
		if err != nil || number < 1 || number > len(m.list.Items()) {
			m.number, m.problem = "", m.answerHint()
			return m, nil
		}
		m.selected, _ = m.list.Items()[number-1].(item)
		m.done = true
		return m, tea.Quit
	}
	highlighted, ok := m.list.SelectedItem().(item)
	if !ok {
		// A filter that matches nothing leaves nothing to take.
		return m, nil
	}
	m.selected, m.done = highlighted, true
	return m, tea.Quit
}

func (m model) answerHint() string {
	return fmt.Sprintf("Answer with a number between 1 and %d.", len(m.list.Items()))
}
