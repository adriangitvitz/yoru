package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var code int
	switch os.Args[1] {
	case "version":
		code = versionCmd()
	case "check":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: yoru check <file.yr>")
			code = 1
		} else {
			code = checkCmd(os.Args[2])
		}
	case "run":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: yoru run <file.yr>")
			code = 1
		} else {
			code = runCmd(os.Args[2])
		}
	case "repl":
		code = replCmd(os.Stdin, os.Stdout, os.Stderr)
	case "fmt":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: yoru fmt <file.yr>")
			code = 1
		} else {
			code = fmtCmd(os.Args[2])
		}
	case "build":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: yoru build --target <mcp|http> [--output <path>] <source.yr>")
			code = 1
		} else {
			code = buildCmd(os.Args[2:])
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		code = 1
	}
	os.Exit(code)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: yoru <command> [arguments]

commands:
  run <file.yr>     Run a Yoru program
  check <file.yr>   Type-check a Yoru program
  build             Build a target (--target mcp|http)
  repl              Start interactive REPL
  fmt <file.yr>     Format a Yoru source file
  version           Print version information`)
}
