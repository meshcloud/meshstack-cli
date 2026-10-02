package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

const currentMarker = "🏠"

type keyMap struct{ move, edit, add, remove, use, quit key.Binding }

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.move, k.edit, k.add, k.remove, k.use, k.quit}
}

func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// keys are checked before the table gets a key, which takes u and d for half a page otherwise.
var keys = keyMap{
	move:   key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "select")),
	edit:   key.NewBinding(key.WithKeys("enter", "e"), key.WithHelp("enter", "edit 📝")),
	add:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add ➕")),
	remove: key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "delete ❌")),
	use:    key.NewBinding(key.WithKeys("u", "space"), key.WithHelp("u", "set "+currentMarker)),
	quit:   key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
}

type model struct {
	profile.Profiles

	ctx context.Context //nolint:containedctx // Update gets no context, and a change is stored from there

	// shown are the profiles in the order of the table's rows.
	shown []*profile.Profile
	table table.Model
	help  help.Model
	// styles are huh's, so that the table looks like the form.
	styles     *huh.Styles
	background tea.BackgroundColorMsg

	width           int
	form            *form
	deletion        *deletion
	quitAfterForm   bool
	detailsMarkdown string
	renderedDetails string
	status          string
	warned          logs.Warned
	err             error
}

type deletion struct {
	*huh.Form

	profile   *profile.Profile
	confirmed bool
}

func newModel(ctx context.Context, profiles profile.Profiles) model {
	m := model{Profiles: profiles, ctx: ctx, table: table.New(table.WithFocused(true)), help: help.New(), width: 80}
	m = m.withStyles(huh.ThemeCharm(false))
	return m.withRows(profiles.CurrentProfile)
}

// Init asks for the background color, which huh's theme picks its colors by.
func (m model) Init() tea.Cmd {
	if m.form != nil {
		return tea.Batch(tea.RequestBackgroundColor, m.startForm())
	}
	return tea.RequestBackgroundColor
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.update(msg)
}

// View uses the alternate screen because the table and the form differ in height, and the inline
// renderer of bubbletea v2.0.10 does not redraw a frame whose height changes.
func (m model) View() tea.View {
	content := m.tableView()
	if m.form != nil {
		content = m.form.View() + "\n" + m.renderedDetails
	}
	content += "\n" + ansi.Truncate(m.warned.String(), m.width, "…") + "\n" + m.status
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

func (m model) withStyles(styles *huh.Styles) model {
	m.styles = styles
	m.help.Styles = styles.Help
	tableStyles := table.DefaultStyles()
	tableStyles.Header = tableStyles.Header.Foreground(styles.Focused.Title.GetForeground())
	tableStyles.Selected = tableStyles.Selected.Foreground(styles.Focused.SelectSelector.GetForeground())
	m.table.SetStyles(tableStyles)
	return m
}

// tableView lays the table out as huh lays out a group, with the title above and the help below.
func (m model) tableView() string {
	body, shownKeys := m.tableBody(), keys
	if len(m.shown) == 0 {
		body = m.styles.Focused.Base.Render(m.styles.Focused.Description.Render("There is no profile yet."))
		for _, binding := range []*key.Binding{&shownKeys.move, &shownKeys.edit, &shownKeys.remove, &shownKeys.use} {
			binding.SetEnabled(false)
		}
	}
	below := m.help.View(shownKeys)
	if m.deletion != nil {
		below = m.dialog(m.deletion.View())
	}
	return m.styles.Group.Title.Render("meshStack CLI profiles") + "\n" + body + "\n\n" + below
}

// tableBody draws huh's bar at the selected row only. The table's Selected style cannot draw it, as
// bubbles' table.Styles has no style for the other rows to keep them in line. Line i+1 is row i
// only while withRows sizes the table to all its rows.
func (m model) tableBody() string {
	lines := strings.Split(m.table.View(), "\n")
	for i, line := range lines {
		if i == m.table.Cursor()+1 {
			lines[i] = m.styles.Focused.Base.Render(line)
		} else {
			lines[i] = m.styles.Blurred.Base.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) dialog(content string) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.styles.Focused.Title.GetForeground()).
		Padding(1, 2).Render(content)
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, box)
}

// startForm loads the details and looks up the workspaces in the background, as both call meshStack.
func (m model) startForm() tea.Cmd {
	original := m.form.original
	ctx, profiles := m.ctx, m.Profiles
	lookup := m.form.lookUpWorkspaces(ctx, profiles)
	if original == nil {
		return tea.Batch(m.form.Init(), lookup)
	}
	return tea.Batch(m.form.Init(), lookup, func() tea.Msg {
		loaded := detailsLoaded{profile: original.Name}
		loaded.markdown, loaded.err = markdown.Execute(showTemplate, showOf(ctx, profiles, original))
		return loaded
	})
}

type detailsLoaded struct {
	profile  profile.Name
	markdown string
	err      error
}

func (m model) withDetails(text string) model {
	m.detailsMarkdown = text
	rendered, err := markdown.Render(text, m.width)
	if err != nil {
		rendered = text
	}
	m.renderedDetails = rendered
	return m
}

func (m model) withRows(highlight profile.Name) model {
	m.shown = m.Profiles.Selection().Profiles
	columns := []table.Column{
		{Width: ansi.StringWidth(currentMarker)}, {Title: "Name"}, {Title: "Endpoint"}, {Title: "Default workspace"},
	}
	rows := make([]table.Row, 0, len(m.shown))
	highlighted := m.table.Cursor()
	for index, p := range m.shown {
		r := table.Row{"", string(p.Name), "", string(p.DefaultWorkspace)}
		if p.Name == m.CurrentProfile {
			r[0] = currentMarker
		}
		if p.Endpoint.URL != nil {
			r[2] = p.Endpoint.String()
		}
		rows = append(rows, r)
		if p.Name == highlight {
			highlighted = index
		}
	}
	for i := range columns {
		columns[i].Width = max(columns[i].Width, ansi.StringWidth(columns[i].Title))
		for _, r := range rows {
			columns[i].Width = max(columns[i].Width, ansi.StringWidth(r[i]))
		}
	}
	// Columns go before rows, as the table renders its rows by the columns it has.
	m.table.SetColumns(columns)
	m.table.SetRows(rows)
	m.table.SetHeight(len(rows) + 1)
	m.table.SetCursor(min(highlighted, len(rows)-1))
	return m
}

func (m model) highlighted() *profile.Profile {
	if cursor := m.table.Cursor(); cursor >= 0 && cursor < len(m.shown) {
		return m.shown[cursor]
	}
	return nil
}

// update is Update keeping the model's type, for the tests.
func (m model) update(msg tea.Msg) (model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.table.SetWidth(msg.Width - m.styles.Focused.Base.GetHorizontalFrameSize())
		if m.detailsMarkdown != "" {
			m = m.withDetails(m.detailsMarkdown)
		}
	case tea.BackgroundColorMsg:
		m.background = msg
		m = m.withStyles(huh.ThemeCharm(msg.IsDark()))
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			if m.quitAfterForm {
				m.err = errFormCancelled
			}
			return m, tea.Quit
		}
	case formSubmitted:
		if d := m.deletion; d != nil {
			m.deletion = nil
			if !d.confirmed {
				return m.finish(d.profile.Name, "", nil)
			}
			return m.finish("", fmt.Sprintf("Deleted profile '%s'.", d.profile.Name), remove(m.ctx, &m.Profiles, d.profile.Name))
		}
		d := *m.form.draft
		// Before save, as the saved profile takes the suggested name, and the next suggestion differs.
		saved := d.savedName(m.Profiles)
		done, err := d.save(m.ctx, &m.Profiles)
		return m.finish(saved, done, err)
	case formCancelled:
		m.deletion = nil
		if m.quitAfterForm {
			return m.finish(m.highlightedName(), "", errFormCancelled)
		}
		return m.finish(m.highlightedName(), "", nil)
	case logs.Warned:
		m.warned = msg
		return m, nil
	case workspacesLookedUp:
		if msg.form != m.form {
			return m, nil
		}
		return m, m.form.onWorkspacesLookedUp(msg)
	case workspaceLookupDue:
		if msg.form != m.form {
			return m, nil
		}
		return m, m.form.lookUpWorkspaces(m.ctx, m.Profiles)
	case detailsLoaded:
		if m.form != nil && m.form.original != nil && m.form.original.Name == msg.profile {
			if msg.err != nil {
				msg.markdown = msg.err.Error()
			}
			m = m.withDetails(msg.markdown)
		}
		return m, nil
	}
	switch {
	case m.deletion != nil:
		_, cmd := m.deletion.Update(msg)
		return m, cmd
	case m.form != nil:
		_, cmd := m.form.Update(msg)
		return m, cmd
	}
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		return m.onKey(msg)
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) onKey(msg tea.KeyPressMsg) (model, tea.Cmd) {
	highlighted := m.highlighted()
	switch {
	case key.Matches(msg, keys.quit):
		return m, tea.Quit
	case key.Matches(msg, keys.add), key.Matches(msg, keys.edit) && highlighted == nil:
		return m.withForm(draft{})
	case key.Matches(msg, keys.edit):
		return m.withForm(draftOf(highlighted))
	case key.Matches(msg, keys.remove) && highlighted != nil:
		return m.withDeletion(highlighted)
	case key.Matches(msg, keys.use) && highlighted != nil:
		return m.finish(highlighted.Name, fmt.Sprintf("Profile '%s' is the current one.", highlighted.Name),
			m.SetCurrent(m.ctx, highlighted.Name))
	case key.Matches(msg, keys.remove, keys.use):
		return m, nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// withDeletion keeps the profile unless Delete is chosen: Enter takes Keep, the button huh focuses
// first.
func (m model) withDeletion(p *profile.Profile) (model, tea.Cmd) {
	d := &deletion{profile: p}
	confirm := huh.NewConfirm().Title(fmt.Sprintf("❌ Delete profile '%s'?", p.Name)).
		Description("Its stored credentials go as well.").Affirmative("Delete").Negative("Keep").Value(&d.confirmed)
	d.Form = huh.NewForm(huh.NewGroup(confirm)).WithKeyMap(formKeyMap()).WithWidth(50)
	d.SubmitCmd = func() tea.Msg { return formSubmitted{} }
	d.CancelCmd = func() tea.Msg { return formCancelled{} }
	m.deletion, m.status = d, ""
	return m, tea.Batch(d.Init(), m.replayBackground())
}

// withForm returns the command that starts the form. A caller that has not started the program yet
// drops it, as Init returns the same command.
func (m model) withForm(d draft) (model, tea.Cmd) {
	f := newForm(m.Profiles, d)
	m.form, m.status = &f, ""
	m.detailsMarkdown, m.renderedDetails = "", ""
	return m, tea.Batch(m.startForm(), m.replayBackground())
}

// replayBackground gives a form opened after the program started the background color, which the
// terminal reports only once.
func (m model) replayBackground() tea.Cmd {
	if m.background.Color == nil {
		return nil
	}
	background := m.background
	return func() tea.Msg { return background }
}

// errFormCancelled fails add and edit as the line prompts fail at the end of their input, so that
// a script does not go on as though the profile was stored.
var errFormCancelled = errors.New("the form was cancelled, nothing was stored")

func (m model) finish(highlight profile.Name, done string, err error) (model, tea.Cmd) {
	m.form, m.detailsMarkdown, m.renderedDetails = nil, "", ""
	switch {
	case err != nil:
		m.status = err.Error()
	case done != "":
		m.status = done
		slog.InfoContext(m.ctx, done)
	}
	m = m.withRows(highlight)
	if m.quitAfterForm {
		m.err = err
		return m, tea.Quit
	}
	return m, nil
}

func (m model) highlightedName() profile.Name {
	if highlighted := m.highlighted(); highlighted != nil {
		return highlighted.Name
	}
	return ""
}

func run(ctx context.Context, p prompt.Prompt, m model) error {
	result, err := p.RunOnTerminal(ctx, m)
	if err != nil {
		return err
	}
	return result.err
}
