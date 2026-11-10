// Command epm-cli is a command-line interface for the BeyondTrust EPM
// for Windows and Mac Management API.
package main

import (
	"fmt"
	"os"

	"github.com/droidnik/epm-cli/internal/cli"
)

func main() {
	args := os.Args[1:]
	// EPM_CLI_ARGS prepends default flags to every invocation.
	if extra := os.Getenv("EPM_CLI_ARGS"); extra != "" {
		pre, err := cli.SplitArgs(extra)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: EPM_CLI_ARGS:", err)
			os.Exit(2)
		}
		args = append(pre, args...)
	}
	os.Exit(cli.NewApp().Execute(args))
}
