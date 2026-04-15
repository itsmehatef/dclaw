package main

import (
	"fmt"
	"os"

	"github.com/itsmehatef/dclaw/internal/cli"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "dclaw: panic: %v\n", r)
			os.Exit(1)
		}
	}()

	if err := cli.Execute(); err != nil {
		// cobra already printed the user-facing error; just exit.
		os.Exit(1)
	}
}
