package prompt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/internal/logs"
)

type Candidate interface {
	Label() string
}

// Select takes the only candidate without asking. Otherwise, it lets the person pick one from a
// list on the terminal, or, where in or out is no terminal, lists the candidates and asks until it
// gets the number of one. An empty answer takes the one isDefault reports, if any.
// An abandoned prompt is an error rather than no choice at all: a login that stored none leaves
// every later command without one.
func Select[C Candidate](ctx context.Context, p Prompt, what string, candidates []C, isDefault func(C) bool) (selected C, err error) {
	switch len(candidates) {
	case 0:
		return selected, errors.New("no " + what + " to select from")
	case 1:
		slog.InfoContext(ctx, fmt.Sprintf("Auto-selecting the only %s available: %s", what, candidates[0].Label()))
		return candidates[0], nil
	}

	items, defaultNumber := make([]item, 0, len(candidates)), 0
	for index, candidate := range candidates {
		number := index + 1
		if defaultNumber == 0 && isDefault != nil && isDefault(candidate) {
			defaultNumber = number
		}
		entry := newItem(number, candidate.Label(), number == defaultNumber)
		if p.UsesTerminal() {
			entry.name = markdown.RenderInline(entry.name)
		}
		items = append(items, entry)
	}
	m := newModel(what, items, defaultNumber)
	if p.UsesTerminal() {
		m, err = selectOnTerminal(ctx, p, m)
	} else {
		m, err = selectByLine(ctx, p, m)
	}
	if err != nil {
		return selected, err
	}
	if m.aborted {
		return selected, fmt.Errorf("no %s was selected", what)
	}
	return candidates[m.selected.number-1], nil
}

// newItem splits off a "(...)" at the end of the label, as a workspace label has for its
// identifier, so that the delegate can line it up.
func newItem(number int, label string, isDefault bool) item {
	if name, detail, found := strings.CutLast(label, " ("); found && strings.HasSuffix(detail, ")") {
		return item{number: number, name: name, detail: " (" + detail, isDefault: isDefault}
	}
	return item{number: number, name: label, isDefault: isDefault}
}

func selectByLine(ctx context.Context, p Prompt, m model) (model, error) {
	for _, listItem := range m.list.Items() {
		candidate, _ := listItem.(item)
		if err := p.Printf(" %s\n", m.delegate.entry(candidate)); err != nil {
			return m, err
		}
	}
	question := fmt.Sprintf("Select a %s [1-%d]: ", m.what, len(m.list.Items()))
	if m.defaultNumber > 0 {
		question = fmt.Sprintf("Select a %s [1-%d, default=%d]: ", m.what, len(m.list.Items()), m.defaultNumber)
	}
	for !m.done {
		if err := p.Printf("%s", question); err != nil {
			return m, err
		}
		answer, err := p.Next(ctx, m.what+" selection")
		if err != nil {
			return m, err
		}
		if strings.Trim(answer, "0123456789") != "" || answer == "" && m.defaultNumber == 0 {
			m.problem = m.answerHint()
		} else {
			for _, digit := range answer {
				m, _ = m.update(tea.KeyPressMsg{Code: digit, Text: string(digit)})
			}
			m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		if m.problem != "" {
			if err := p.Printf("%s\n", m.problem); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

func selectOnTerminal(ctx context.Context, p Prompt, m model) (model, error) {
	// Sized before its first frame, the list does not shrink when the program reports the size.
	if file, ok := p.out.(*os.File); ok {
		if width, height, err := term.GetSize(file.Fd()); err == nil {
			m, _ = m.update(tea.WindowSizeMsg{Width: width, Height: height})
		}
	}
	result, err := p.RunOnTerminal(ctx, m)
	if err != nil {
		return m, err
	}
	// The renderer leaves the cursor on the last line of the frame, which the outcome replaces.
	cursorUp := ""
	if frameHeight := strings.Count(result.View().Content, "\n") + 1; frameHeight > 1 {
		cursorUp = ansi.CursorUp(frameHeight - 1)
	}
	return result, p.Printf("\r%s%s%s\n", cursorUp, ansi.EraseScreenBelow, result.outcome())
}

func (p Prompt) RunOnTerminal[M tea.Model](ctx context.Context, m M) (M, error) {
	finished, err := RunProgram(ctx, tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(p.input.in), tea.WithOutput(p.out)))
	if err != nil {
		return m, err
	}
	result, ok := finished.(M)
	if !ok {
		return m, fmt.Errorf("the terminal UI ended with an unexpected %T", finished)
	}
	return result, nil
}

// RunProgram holds back the log while program runs, and sends program a logs.Warned after each
// record at WARN or above. Once program has ended, the log gets every record it held.
func RunProgram(ctx context.Context, program *tea.Program) (tea.Model, error) {
	held := logs.Hold(func(warned logs.Warned) { program.Send(warned) })
	defer held.Release(ctx)
	return program.Run()
}
