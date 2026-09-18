package setting_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/setting/setting_test"
)

func TestResolveSettingPrecedence(t *testing.T) {
	staticSource := func(value string) setting.Source {
		return setting_test.LookupFunc(func(_ context.Context, _ string) (string, error) {
			return value, nil
		})
	}
	precedence := setting.Setting[string]{
		Env:     "MESHSTACK_TEST_PRECEDENCE",
		Default: setting.StaticDefault("from the default"),
		Parse:   setting.ParseText[string],
	}
	var (
		frontend = setting.FrontendSource{Source: staticSource("from the front end")}
		other    = staticSource("from another source")
		fallback = setting.FallbackSource{Source: staticSource("from the fallback")}
	)

	tests := []struct {
		name    string
		sources setting.Sources
		extra   setting.Sources
		env     string
		want    string
	}{
		{
			name:    "a front end source outranks every other kind",
			sources: setting.Sources{fallback, other, frontend},
			env:     "from the environment",
			want:    "from the front end",
		},
		{
			name:    "an extra source ranks as the sources it is resolved with",
			sources: setting.Sources{fallback},
			extra:   setting.Sources{frontend},
			env:     "from the environment",
			want:    "from the front end",
		},
		{
			name:    "another source outranks the environment",
			sources: setting.Sources{fallback, other},
			env:     "from the environment",
			want:    "from another source",
		},
		{
			name:    "the environment outranks a fallback source",
			sources: setting.Sources{fallback},
			env:     "from the environment",
			want:    "from the environment",
		},
		{
			name:    "a fallback source outranks the default",
			sources: setting.Sources{fallback},
			want:    "from the fallback",
		},
		{
			name: "the default is last",
			want: "from the default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// An empty value is skipped as no value at all, so this is also the unset case.
			t.Setenv(precedence.EnvKey(), tt.env)
			value, err := tt.sources.ResolveSetting(t.Context(), precedence, tt.extra...)
			require.NoError(t, err)
			assert.Equal(t, tt.want, value)
		})
	}
}
