package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/stdlib"
)

func replCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	interp := interpreter.NewInterpreter()
	stdlib.InstallAll(interp, stderr)
	scanner := bufio.NewScanner(stdin)

	_, _ = fmt.Fprintf(stderr, "Yoru %s REPL — type :help for commands, :exit to quit\n", Version)

	var buffer strings.Builder
	depth := 0

	for {
		if depth == 0 {
			_, _ = fmt.Fprint(stderr, ">>> ")
		} else {
			_, _ = fmt.Fprint(stderr, "... ")
		}

		if !scanner.Scan() {
			if buffer.Len() > 0 {
				evalREPL(interp, buffer.String(), stdout, stderr)
			}
			_, _ = fmt.Fprintln(stderr)
			return 0
		}

		line := scanner.Text()

		// Meta-commands only fire at depth 0 so they can't shadow identifiers in blocks.
		if depth == 0 {
			trimmed := strings.TrimSpace(line)
			if trimmed == ":exit" {
				return 0
			}
			if trimmed == ":help" {
				_, _ = fmt.Fprintln(stdout, `Commands:
  :exit    Exit the REPL
  :help    Show this help
  :reset   Reset the environment`)
				continue
			}
			if trimmed == ":reset" {
				interp.Reset()
				_, _ = fmt.Fprintln(stderr, "Environment reset.")
				continue
			}
		}

		buffer.WriteString(line)
		buffer.WriteString("\n")

		// Naive brace balance: strings with literal {/} are rare interactively.
		depth += strings.Count(line, "{") - strings.Count(line, "}")

		if depth <= 0 {
			depth = 0
			evalREPL(interp, buffer.String(), stdout, stderr)
			buffer.Reset()
		}
	}
}

func evalREPL(interp *interpreter.Interpreter, src string, stdout io.Writer, stderr io.Writer) {
	src = strings.TrimSpace(src)
	if src == "" {
		return
	}

	// Recover so a panic in one expression doesn't kill the session.
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(stderr, "runtime error: %v\n", r)
		}
	}()

	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			_, _ = fmt.Fprintf(stderr, "parse error: %s\n", e)
		}
		return
	}

	result, err := interp.EvalProgram(prog)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return
	}

	if result != nil && result.LastValue != nil {
		if _, ok := result.LastValue.(*interpreter.NilVal); !ok {
			_, _ = fmt.Fprintln(stdout, result.LastValue.Inspect())
		}
	}
}
