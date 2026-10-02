package profile

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

// draft is a profile being edited, or being added where original is nil.
type draft struct {
	name, endpoint, workspace string

	original *profile.Profile
}

func draftOf(original *profile.Profile) draft {
	d := draft{name: string(original.Name), workspace: string(original.DefaultWorkspace), original: original}
	if original.Endpoint.URL != nil {
		d.endpoint = original.Endpoint.String()
	}
	return d
}

type question struct {
	title, description string
	value              *string
	validate           func(string) error
	suggestions        []string
	// placeholder is the answer an empty value stands for.
	placeholder func() string
	// suggestedLater are suggestions that the form gets once a lookup in the background finds them.
	suggestedLater bool
}

func (d *draft) questions(profiles profile.Profiles) []question {
	endpointHint := "The meshStack to log in to."
	if d.original != nil {
		endpointHint += " A new endpoint logs the profile out."
	}
	// localEndpoint comes last, so that a known https endpoint wins over it for a prefix of both,
	// such as "http".
	endpoints := append(slices.DeleteFunc(profiles.Endpoints(), func(endpoint string) bool {
		return endpoint == localEndpoint
	}), localEndpoint)
	return []question{
		{
			title: "Endpoint", description: endpointHint, value: &d.endpoint, suggestions: endpoints,
			validate: func(value string) error {
				_, err := parseEndpoint(value)
				return err
			},
		},
		{
			title: "Default workspace", description: "Optional. The workspace a command works in unless --workspace names one.",
			value: &d.workspace, suggestedLater: true,
		},
		{
			title: "Name", value: &d.name, validate: d.validateName(profiles),
			placeholder: func() string { return d.suggestedName(profiles) },
		},
	}
}

func (d *draft) validateName(profiles profile.Profiles) func(string) error {
	return func(value string) error {
		var name profile.Name
		if err := name.UnmarshalText([]byte(cmp.Or(value, d.suggestedName(profiles)))); err != nil {
			return err
		}
		return profiles.CheckNameFree(name, d.original)
	}
}

func parseEndpoint(value string) (endpoint xurl.URL, err error) {
	if value == "" {
		return endpoint, errors.New("a profile needs an endpoint")
	}
	err = endpoint.UnmarshalText([]byte(value))
	return
}

// localEndpoint is meshfed-api of the local stack in ../meshfed-release, on Spring's default port.
const localEndpoint = "http://localhost:8080"

func (d *draft) suggestedName(profiles profile.Profiles) string {
	parsed, err := parseEndpoint(d.endpoint)
	if err != nil {
		return ""
	}
	base := "dev-local"
	if parsed.Scheme != "http" {
		labels := strings.Split(parsed.Hostname(), ".")
		kept := slices.DeleteFunc(slices.Clone(labels[:max(len(labels)-1, 1)]), func(label string) bool {
			return label == "federation" || label == "api"
		})
		if len(kept) == 0 {
			kept = labels[:1]
		}
		base = strings.Join(kept, "-")
	}
	name := base
	for n := 2; profiles.Profiles[profile.Name(name)] != nil; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
}

// savedName is the name that save stores the profile under, as long as profiles do not change.
func (d *draft) savedName(profiles profile.Profiles) profile.Name {
	return profile.Name(cmp.Or(d.name, d.suggestedName(profiles)))
}

func (d *draft) save(ctx context.Context, profiles *profile.Profiles) (done string, err error) {
	var edited profile.Profile
	if d.original != nil {
		edited = *d.original
	}
	if err = edited.Name.UnmarshalText([]byte(d.savedName(*profiles))); err != nil {
		return
	}
	if edited.Endpoint, err = parseEndpoint(d.endpoint); err != nil {
		return
	}
	edited.DefaultWorkspace = meshstack.Workspace(d.workspace)
	if err = profiles.Put(ctx, d.original, edited); err != nil {
		return
	}
	if d.original == nil {
		return fmt.Sprintf("Added profile '%s'.", edited.Name), nil
	}
	return fmt.Sprintf("Changed profile '%s'.", edited.Name), nil
}

func (d *draft) ask(ctx context.Context, p prompt.Prompt, profiles profile.Profiles) error {
	for _, q := range d.questions(profiles) {
		answer := *q.value
		if q.placeholder != nil {
			answer = cmp.Or(answer, q.placeholder())
		}
		answer, err := p.Ask(ctx, q.title, answer, q.validate)
		if err != nil {
			return err
		}
		*q.value = answer
	}
	return nil
}

type (
	formSubmitted struct{}
	formCancelled struct{}
)

type form struct {
	*huh.Form
	*draft

	workspaceInput *huh.Input
	// workspacesFor is the endpoint whose workspaces workspaceInput suggests.
	workspacesFor string
	// placeholderInput shows placeholder, which Update sets anew: huh runs a PlaceholderFunc outside
	// the program's loop, where reading the draft races with Update writing it.
	placeholderInput *huh.Input
	placeholder      func() string
}

func newForm(profiles profile.Profiles, d draft) form {
	f := form{draft: &d}
	title := "➕ Add a profile"
	if d.original != nil {
		title = fmt.Sprintf("📝 Edit profile %s", d.original.Name)
	}
	questions := f.questions(profiles)
	fields := make([]huh.Field, 0, len(questions))
	for _, q := range questions {
		input := huh.NewInput().Title(q.title).Description(q.description).Value(q.value)
		if q.validate != nil {
			input.Validate(q.validate)
		}
		if q.placeholder != nil {
			f.placeholderInput, f.placeholder = input.Placeholder(q.placeholder()), q.placeholder
		}
		switch {
		case q.suggestedLater:
			f.workspaceInput = input
			fields = append(fields, suggestingInput{input})
		case len(q.suggestions) > 0:
			fields = append(fields, suggestingInput{input.Suggestions(q.suggestions)})
		default:
			fields = append(fields, input)
		}
	}
	f.Form = huh.NewForm(huh.NewGroup(fields...).Title(title)).WithKeyMap(formKeyMap())
	f.SubmitCmd = func() tea.Msg { return formSubmitted{} }
	f.CancelCmd = func() tea.Msg { return formCancelled{} }
	return f
}

func (f *form) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	model, cmd := f.Form.Update(msg)
	f.placeholderInput.Placeholder(f.placeholder())
	return model, cmd
}

// formKeyMap makes Esc cancel the form, as Ctrl-C quits the whole program in model.update.
func formKeyMap() *huh.KeyMap {
	keyMap := huh.NewDefaultKeyMap()
	keyMap.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	keyMap.Input.AcceptSuggestion.SetHelp(completeKey, "complete")
	return keyMap
}

const completeKey = "tab"

// suggestingInput completes a suggestion on Tab, as a shell does, and moves on only when there is
// nothing to complete. huh binds Tab to the next field and completes on ctrl+e alone. It also adds
// the keys that pick a suggestion, which huh leaves out of the help.
type suggestingInput struct {
	*huh.Input
}

var acceptSuggestion = tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}

// KeyBinds shows the keys of the suggestions only once there are any, as huh does for its own key.
func (i suggestingInput) KeyBinds() []key.Binding {
	binds := i.Input.KeyBinds()
	if !slices.ContainsFunc(binds, func(b key.Binding) bool { return b.Help().Key == completeKey }) {
		return binds
	}
	pick := key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "pick a known one"))
	return append([]key.Binding{pick}, binds...)
}

// Update keeps the wrapper, as the group stores the field that Update returns.
func (i suggestingInput) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	if keyPress, isKey := msg.(tea.KeyPressMsg); isKey && keyPress.String() == completeKey {
		before := i.GetValue()
		if _, cmd := i.Input.Update(acceptSuggestion); i.GetValue() != before {
			return i, cmd
		}
	}
	_, cmd := i.Input.Update(msg)
	return i, cmd
}

// workspaceLookupTimeout keeps a lookup from holding back the next, as the form retries every
// workspaceLookupInterval.
const (
	workspaceLookupTimeout  = 100 * time.Millisecond
	workspaceLookupInterval = 500 * time.Millisecond
)

// The lookup messages name their form, so that the lookups of a form that has closed stop, even
// where another form has opened since.
type (
	workspacesLookedUp struct {
		form     *form
		endpoint string
		names    []string
	}
	workspaceLookupDue struct{ form *form }
)

// lookUpWorkspaces copies the endpoint and the profiles to ask here, as the command runs outside
// the program's loop, which changes both. It looks up nothing once the suggestions are those of
// the endpoint.
func (f *form) lookUpWorkspaces(ctx context.Context, profiles profile.Profiles) tea.Cmd {
	if f.endpoint == f.workspacesFor {
		return f.nextWorkspaceLookup()
	}
	endpoint := f.endpoint
	var asked []profile.Profile
	if parsed, err := parseEndpoint(endpoint); err == nil {
		asked = profiles.MatchingEndpoint(parsed)
	}
	return func() tea.Msg {
		return workspacesLookedUp{form: f, endpoint: endpoint, names: knownWorkspaces(ctx, asked)}
	}
}

func (f *form) onWorkspacesLookedUp(msg workspacesLookedUp) tea.Cmd {
	if msg.endpoint == f.endpoint && len(msg.names) > 0 {
		f.workspaceInput.Suggestions(msg.names)
		f.workspacesFor = msg.endpoint
	}
	return f.nextWorkspaceLookup()
}

func (f *form) nextWorkspaceLookup() tea.Cmd {
	return tea.Tick(workspaceLookupInterval, func(time.Time) tea.Msg { return workspaceLookupDue{form: f} })
}

// knownWorkspaces lists the workspaces that the stored credentials of profiles can reach. A failure
// only leaves the suggestions out, so it is logged at debug level.
func knownWorkspaces(ctx context.Context, profiles []profile.Profile) []string {
	ctx, cancel := context.WithTimeout(ctx, workspaceLookupTimeout)
	defer cancel()
	var names []string
	for _, p := range profiles {
		workspaces, err := listWorkspaces(ctx, &p)
		if err != nil {
			slog.DebugContext(ctx, fmt.Sprintf("Cannot suggest the workspaces of profile '%s': %s", p.Name, err))
			continue
		}
		for _, w := range workspaces {
			names = append(names, w.Metadata.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func listWorkspaces(ctx context.Context, p *profile.Profile) ([]client.MeshWorkspace, error) {
	session, err := auth.StoredSession(ctx, p, internal.ResolveClientOptions(internal.SkipVersionCheck))
	if err != nil {
		return nil, err
	}
	c, err := session.Client()
	if err != nil {
		return nil, err
	}
	return c.Workspace.List(ctx)
}
