package main

import (
	"fmt"
	"os"

	"github.com/opsd-labs/opsd/internal/cli"
)

func main() {
	if err := cli.RunControl(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
