package internal

import "github.com/meshcloud/meshstack-cli/internal/setting"

type FlagSource struct {
	MatchingEnvKey string
}

func (f FlagSource) Lookup(key string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FlagSource) Describe(key string) setting.SourceDescription {
	//TODO implement me
	panic("implement me")
}
