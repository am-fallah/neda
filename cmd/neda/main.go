// Command neda runs the Neda service and its small operational commands.
package main

import (
	"fmt"
	"io"
	"os"
	_ "time/tzdata" // Embed IANA zones so Asia/Tehran works in minimal images.
)

// These values are overridden at build time with -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithInput(args, os.Stdin, stdout, stderr)
}

func runWithInput(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "serve":
		return runServe(stdout, stderr)
	case "set-password":
		return runSetPassword(args[1:], stdout, stderr, termPrompter{in: stdin})
	case "healthcheck":
		return runHealthcheck(stdout, stderr)
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "neda %s (commit %s, built %s)\n", version, commit, date)
		return 0
	case "help", "--help", "-h":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "neda: unknown command %q\n\n", command)
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: neda <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  serve          Run the Neda service (default)")
	fmt.Fprintln(w, "  set-password   Set the owner password interactively (TTY only)")
	fmt.Fprintln(w, "  healthcheck    Probe the local health endpoint")
	fmt.Fprintln(w, "  version        Print build version information")
	fmt.Fprintln(w, "  help           Print this help")
}
