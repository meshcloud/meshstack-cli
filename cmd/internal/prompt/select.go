package prompt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
)

// Candidate is one thing a person can select.
type Candidate interface {
	// Label is what the person selects it by.
	Label() string
}

// Select takes the only candidate without asking. Otherwise, it lists the candidates and asks
// until it gets the number of one, and an empty answer takes the one isDefault reports, if any.
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

	defaultNumber := 0
	defaultQuestionMarker := ""
	for index, candidate := range candidates {
		number, defaultMarker := index+1, " "
		if defaultNumber == 0 && isDefault != nil && isDefault(candidate) {
			defaultNumber, defaultMarker = number, "*"
			defaultQuestionMarker = fmt.Sprintf(", default=%d", number)
		}
		if err := p.Printf(" %s[%d] %s\n", defaultMarker, number, candidate.Label()); err != nil {
			return selected, err
		}
	}

	question := fmt.Sprintf("Select a %s [1-%d%s]: ", what, len(candidates), defaultQuestionMarker)
	for {
		if err := p.Printf("%s", question); err != nil {
			return selected, err
		}
		answer, err := p.Next(ctx, what+" selection")
		if err != nil {
			return selected, err
		}
		// An empty answer takes the marked default, and asks again where there is none: zero is
		// out of range below.
		number, convErr := defaultNumber, error(nil)
		if answer != "" {
			number, convErr = strconv.Atoi(answer)
		}
		if convErr != nil || number < 1 || number > len(candidates) {
			if err := p.Printf("Answer with a number between 1 and %d.\n", len(candidates)); err != nil {
				return selected, err
			}
			continue
		}
		return candidates[number-1], nil
	}
}
