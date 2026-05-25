package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/typechecker"
)

func checkCmd(filename string) int {
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", filename, err)
		return 1
	}
	src := string(data)

	var errors []string

	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	for _, e := range p.Errors() {
		errors = append(errors, fmt.Sprintf("%s: parse error: %s", filename, e))
	}

	if len(p.Errors()) == 0 && prog != nil {
		c := typechecker.NewChecker()
		res := c.Check(prog)
		for _, e := range res.Errors {
			msg := strings.TrimPrefix(e, "parse error: ")
			errors = append(errors, fmt.Sprintf("%s: type error: %s", filename, msg))
		}
	}

	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Fprintln(os.Stderr, e)
		}
		return 2
	}

	fmt.Printf("%s: ok\n", filename)
	return 0
}
