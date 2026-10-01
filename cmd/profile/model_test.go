package profile

import (
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

var (
	enter  = tea.KeyPressMsg{Code: tea.KeyEnter}
	escape = tea.KeyPressMsg{Code: tea.KeyEscape}
	down   = tea.KeyPressMsg{Code: tea.KeyDown}
	tab    = tea.KeyPressMsg{Code: tea.KeyTab}
)

func press(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func typed(text string) (keys []tea.Msg) {
	for _, r := range text {
		keys = append(keys, press(r))
	}
	return
}

func twoProfiles(t *testing.T) profile.Profiles {
	t.Helper()
	return storedProfiles(t,
		profile.Profile{Name: "dev", Endpoint: endpointA},
		profile.Profile{Name: "prod", Endpoint: endpointB, DefaultWorkspace: "ops"},
	)
}

func TestListKeysChangeTheHighlightedProfileAndStoreItAtOnce(t *testing.T) {
	tests := []struct {
		name        string
		keys        []tea.Msg
		wantNames   []profile.Name
		wantCurrent profile.Name
		wantLogged  []string
	}{
		{
			name:        "d and y delete the highlighted profile",
			keys:        []tea.Msg{down, press('d'), press('y')},
			wantNames:   []profile.Name{"dev"},
			wantCurrent: "dev",
			wantLogged:  []string{"Deleted profile 'prod'."},
		},
		{
			name:        "deleting the current profile makes the only one left the current one",
			keys:        []tea.Msg{press('d'), press('y')},
			wantNames:   []profile.Name{"prod"},
			wantCurrent: "prod",
			wantLogged:  []string{"Profile 'prod' is the current one now, as it is the only one left.", "Deleted profile 'dev'."},
		},
		{
			name:        "d and enter keep it, as Keep is focused",
			keys:        []tea.Msg{press('d'), enter},
			wantNames:   []profile.Name{"dev", "prod"},
			wantCurrent: "dev",
		},
		{
			name:        "d and escape keep it",
			keys:        []tea.Msg{press('d'), escape},
			wantNames:   []profile.Name{"dev", "prod"},
			wantCurrent: "dev",
		},
		{
			name:        "u makes the highlighted profile the current one",
			keys:        []tea.Msg{down, press('u')},
			wantNames:   []profile.Name{"dev", "prod"},
			wantCurrent: "prod",
			wantLogged:  []string{"Profile 'prod' is the current one."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profiles := twoProfiles(t)
			captured := logs.Capture(t)
			synctest.Test(t, func(t *testing.T) {
				m, quitItself := drive(t, newModel(t.Context(), profiles), tt.keys...)

				assert.False(t, quitItself)
				assert.Nil(t, m.deletion, "the dialog closes")
				assert.Equal(t, tt.wantNames, names(m.Profiles))
				assert.Equal(t, tt.wantCurrent, m.CurrentProfile)
				assert.Equal(t, tt.wantLogged, logged(captured))
				reloaded, err := profile.LoadProfiles(t.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
				require.NoError(t, err)
				assert.Equal(t, tt.wantNames, names(reloaded), "every change is stored at once")
			})
		})
	}
}

func TestTheTableMarksTheCurrentProfileAndADialogAsksBeforeItDeletes(t *testing.T) {
	m := newModel(t.Context(), twoProfiles(t))
	m, _ = m.update(tea.WindowSizeMsg{Width: 80, Height: 20})

	view := m.View()

	assert.True(t, view.AltScreen)
	content := ansi.Strip(view.Content)
	assert.Regexp(t, `Name +Endpoint +Default workspace`, content)
	assert.Regexp(t, `🏠 +dev +https://a.example.io`, content)
	assert.Regexp(t, `prod +https://b.example.io +ops`, content)
	assert.Contains(t, content, "u set 🏠")
	assert.NotContains(t, content, "…", "the help fits on 80 columns")

	m, _ = m.update(press('d'))
	assert.Contains(t, ansi.Strip(m.View().Content), "Delete profile 'dev'?")
}

func TestWithoutProfilesTheHelpOffersOnlyToAdd(t *testing.T) {
	content := ansi.Strip(newModel(t.Context(), storedProfiles(t)).View().Content)

	assert.Contains(t, content, "There is no profile yet.")
	assert.Contains(t, content, "a add ➕ • q quit")
	assert.NotContains(t, content, "edit")
}

func TestQEscAndCtrlCQuitTheList(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{press('q'), escape, {Code: 'c', Mod: tea.ModCtrl}} {
		t.Run(key.String(), func(t *testing.T) {
			m := newModel(t.Context(), twoProfiles(t))

			_, cmd := m.update(key)

			require.NotNil(t, cmd)
			assert.Equal(t, tea.QuitMsg{}, cmd())
		})
	}
}

func TestEnterEditsTheHighlightedProfileAndAAddsOne(t *testing.T) {
	m := newModel(t.Context(), twoProfiles(t))

	edit, _ := m.update(enter)
	require.NotNil(t, edit.form)
	assert.Equal(t, profile.Name("dev"), edit.form.original.Name)
	assert.Equal(t, "https://a.example.io", edit.form.endpoint)

	add, _ := m.update(press('a'))
	require.NotNil(t, add.form)
	assert.Nil(t, add.form.original)
}

// drive runs m in a program, typing the keys once everything the previous one started has settled.
// It quits the program where the model did not, and reports whether the model did.
func drive(t *testing.T, m model, keys ...tea.Msg) (result model, quitItself bool) {
	t.Helper()
	program := tea.NewProgram(m, tea.WithContext(t.Context()), tea.WithInput(nil), tea.WithOutput(io.Discard),
		tea.WithWindowSize(100, 30), tea.WithoutSignalHandler())
	finished := make(chan tea.Model, 1)
	go func() {
		result, err := prompt.RunProgram(t.Context(), program)
		assert.NoError(t, err)
		finished <- result
	}()
	for _, key := range keys {
		synctest.Wait()
		program.Send(key)
	}
	synctest.Wait()
	select {
	case finishedModel := <-finished:
		quitItself = true
		result, _ = finishedModel.(model)
	default:
		program.Quit()
		result, _ = (<-finished).(model)
	}
	// The cursor of a form blinks on a timer, and the bubble stops its clock once the test ends.
	synctest.Sleep(time.Second)
	return
}

func TestTheFormAddsAProfileAndGoesBackToTheList(t *testing.T) {
	profiles := twoProfiles(t)
	captured := logs.Capture(t)
	synctest.Test(t, func(t *testing.T) {
		// A suggestion of the known endpoints completes the rest: Tab completes it, and a second Tab
		// has nothing left to complete and moves on.
		keys := append(typed("a"), typed("https://b")...)
		keys = append(keys, tab, tab, enter)
		keys = append(keys, typed("staging")...)
		keys = append(keys, enter)

		m, quitItself := drive(t, newModel(t.Context(), profiles), keys...)

		assert.False(t, quitItself, "the list shows again")
		assert.Nil(t, m.form)
		assert.Equal(t, []string{"Added profile 'staging'."}, logged(captured))
		reloaded, err := profile.LoadProfiles(t.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
		require.NoError(t, err)
		require.Contains(t, reloaded.Profiles, profile.Name("staging"))
		assert.Equal(t, endpointB, reloaded.Profiles["staging"].Endpoint)
		assert.Equal(t, profile.Name("dev"), reloaded.CurrentProfile)

		m, _ = drive(t, m, down)
		assert.Equal(t, "Added profile 'staging'.", m.status, "moving the cursor keeps the message")
	})
}

func TestTheFormRefusesATakenNameAndEscGoesBackToTheList(t *testing.T) {
	profiles := twoProfiles(t)
	captured := logs.Capture(t)
	synctest.Test(t, func(t *testing.T) {
		keys := append(typed("a"), typed("https://b")...)
		keys = append(keys, tab, tab, enter)
		keys = append(keys, typed("prod")...)
		keys = append(keys, enter)

		m, _ := drive(t, newModel(t.Context(), profiles), keys...)

		require.NotNil(t, m.form, "the form stays on the name")
		assert.Contains(t, m.View().Content, "a profile named 'prod' exists already")

		m, _ = drive(t, m, escape)
		assert.Nil(t, m.form, "Esc goes back to the list")
		assert.Empty(t, logged(captured))
	})
}

func TestAFormForOneEditEndsTheProgramOnceSaved(t *testing.T) {
	profiles := twoProfiles(t)
	captured := logs.Capture(t)
	synctest.Test(t, func(t *testing.T) {
		m, _ := newModel(t.Context(), profiles).withForm(draftOf(profiles.Profiles["prod"]))
		m.quitAfterForm = true
		keys := []tea.Msg{enter, tea.KeyPressMsg{Code: tea.KeyBackspace}, enter}
		keys = append(keys, typed("uction")...)
		keys = append(keys, enter)

		m, quitItself := drive(t, m, keys...)

		assert.True(t, quitItself, "the program ends with the form")
		require.NoError(t, m.err)
		assert.Equal(t, []string{"Changed profile 'production'."}, logged(captured))
		reloaded, err := profile.LoadProfiles(t.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
		require.NoError(t, err)
		assert.Equal(t, []profile.Name{"dev", "production"}, names(reloaded))
		assert.Equal(t, "op", string(reloaded.Profiles["production"].DefaultWorkspace))
	})
}

func TestAFormForOneEditFailsOnceCancelled(t *testing.T) {
	profiles := twoProfiles(t)
	for name, key := range map[string]tea.Msg{"esc": escape, "ctrl+c": tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}} {
		t.Run(name, func(t *testing.T) {
			captured := logs.Capture(t)
			synctest.Test(t, func(t *testing.T) {
				m, _ := newModel(t.Context(), profiles).withForm(draft{})
				m.quitAfterForm = true

				m, quitItself := drive(t, m, key)

				assert.True(t, quitItself)
				require.ErrorIs(t, m.err, errFormCancelled)
				assert.Empty(t, logged(captured))
			})
		})
	}
}

func TestTheEditFormShowsTheStoredCredentialOnceItIsRead(t *testing.T) {
	profiles := twoProfiles(t)
	t.Setenv("GLAMOUR_STYLE", "notty")
	synctest.Test(t, func(t *testing.T) {
		m, _ := newModel(t.Context(), profiles).withForm(draftOf(profiles.Profiles["prod"]))

		m, _ = drive(t, m)

		require.NotNil(t, m.form)
		assert.Regexp(t, `Credential +\| none`, m.View().Content)
	})
}

func TestAWarningWhileTheProgramRunsShowsInTheViewAndIsLoggedInOrderOnceItEnds(t *testing.T) {
	profiles := storedProfiles(t,
		profile.Profile{Name: "dev", Endpoint: endpointA},
		profile.Profile{Name: "prod", Endpoint: endpointB},
		profile.Profile{Name: "staging", Endpoint: endpointC},
	)
	captured := logs.Capture(t)
	synctest.Test(t, func(t *testing.T) {
		m, _ := drive(t, newModel(t.Context(), profiles), down, press('u'), press('d'), press('y'))

		assert.Contains(t, m.View().Content, "1 warning was logged, read it once you quit: No profile is current now.")
		assert.Equal(t, []string{
			"Profile 'prod' is the current one.",
			"No profile is current now. Make one the current one in meshstack profile, " +
				"or log in to it with meshstack login --profile <name>.",
			"Deleted profile 'prod'.",
		}, logged(captured))
	})
}

func logged(captured *logs.Captured) (messages []string) {
	for _, record := range captured.Records(slog.LevelInfo) {
		messages = append(messages, record.Message)
	}
	return
}

func TestTheHelpShowsTheKeysOfTheKnownEndpointsOnlyInTheEndpointField(t *testing.T) {
	// A valid endpoint, as huh shows a field's error in place of the help.
	f := newForm(twoProfiles(t), draft{endpoint: "https://b.example.io"})
	f.Init()
	help := ansi.Strip(f.View())
	assert.Contains(t, help, "↑/↓ pick a known one")
	assert.Contains(t, help, "tab complete")

	f.NextField()

	assert.NotContains(t, ansi.Strip(f.View()), "pick a known one")
}

func TestAnEmptyNameTakesTheOneSuggestedForALocalEndpoint(t *testing.T) {
	profiles := twoProfiles(t)
	captured := logs.Capture(t)
	synctest.Test(t, func(t *testing.T) {
		keys := append(typed("a"), typed("http:")...)
		keys = append(keys, tab, tab, enter, enter)

		drive(t, newModel(t.Context(), profiles), keys...)

		assert.Equal(t, []string{"Added profile 'dev-local'."}, logged(captured))
	})
}

func TestTheWorkspacesLookedUpForTheEndpointBecomeItsSuggestions(t *testing.T) {
	m, _ := newModel(t.Context(), twoProfiles(t)).withForm(draft{endpoint: "https://b.example.io"})
	stale := &form{}

	m, _ = m.update(workspacesLookedUp{form: stale, endpoint: "https://b.example.io", names: []string{"stale"}})
	m, _ = m.update(workspacesLookedUp{form: m.form, endpoint: "https://b.example.io", names: []string{"ops"}})
	m.form.NextField()

	help := ansi.Strip(m.form.View())
	assert.Contains(t, help, "↑/↓ pick a known one")
	assert.Equal(t, "https://b.example.io", m.form.workspacesFor)
}
