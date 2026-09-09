package main

import (
	"fmt"
	"io"
	"os"
)

const notice = "meshstack has no commands yet. This build only proves that the repository builds, tests and ships."

func run(out io.Writer) error {
	_, err := fmt.Fprintln(out, notice)
	return err
}

func main() {
	if err := run(os.Stdout); err != nil {
		os.Exit(1)
	}
}
