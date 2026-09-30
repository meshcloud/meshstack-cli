// Command fakemeshstack stands in for three meshStack installations and their Keycloak, so that
// docs/demo/demo.tape can record `meshstack login` with no backend and no network.
//
//	fakemeshstack serve <dir>   serves them, and writes the demo's config and environment to <dir>
//	xdg-open <url>              a symlink to this binary: plays the person in the browser
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var err error
	switch {
	case filepath.Base(os.Args[0]) == "xdg-open" && len(os.Args) == 2:
		err = browse(os.Args[1])
	case len(os.Args) == 3 && os.Args[1] == "serve":
		err = serve(os.Args[2])
	default:
		err = fmt.Errorf("usage: %s serve <dir>, or as xdg-open <url>", os.Args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
