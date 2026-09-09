package main

import (
	"strings"
	"testing"
)

func TestRunWritesOneLineToItsWriter(t *testing.T) {
	var out strings.Builder

	if err := run(&out); err != nil {
		t.Fatalf("run returned %v", err)
	}

	if got, want := out.String(), notice+"\n"; got != want {
		t.Errorf("run wrote %q, want %q", got, want)
	}
}
