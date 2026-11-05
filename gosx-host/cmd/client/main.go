// Build the Go-authored editor runtime with the pinned GoSX Go-WASM builder.
package main

import (
	"fmt"
	"os"

	"github.com/jvcencio/storyboard-desk/gosx-host/internal/clientbuild"
)

func main() {
	dir, err := os.Getwd()
	if err == nil {
		err = clientbuild.Build(dir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
