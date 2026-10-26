package main

import (
	"fmt"
	"os"

	"github.com/opsd-labs/opsd/internal/agentcli"
)

func main() {
	if err := agentcli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
