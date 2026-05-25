package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
)

// Keywords that get a blank line inserted before them at the top level.
var topLevelKeywords = map[string]bool{
	"fn": true, "object": true, "enum": true, "actor": true,
	"pipeline": true, "tool": true, "protocol": true, "impl": true,
}

func fmtCmd(filename string) int {
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", filename, err)
		return 1
	}
	src := string(data)

	// Refuse to rewrite unparseable source.
	l := lexer.New(src)
	p := parser.New(l)
	p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			fmt.Fprintf(os.Stderr, "%s: parse error: %s\n", filename, e)
		}
		return 2
	}

	formatted := formatSource(src)

	if err := os.WriteFile(filename, []byte(formatted), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", filename, err)
		return 1
	}
	return 0
}

func formatSource(src string) string {
	lines := strings.Split(src, "\n")
	var result []string
	depth := 0
	prevWasBlank := false
	prevDepth := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if !prevWasBlank && len(result) > 0 {
				result = append(result, "")
				prevWasBlank = true
			}
			continue
		}
		prevWasBlank = false

		if strings.HasPrefix(trimmed, "}") {
			depth--
			if depth < 0 {
				depth = 0
			}
		}

		// Only separate top-level decls; nested ones must not gain blank lines.
		if depth == 0 && i > 0 && len(result) > 0 {
			firstWord := strings.Fields(trimmed)[0]
			if topLevelKeywords[firstWord] && !prevWasBlank && prevDepth == 0 {
				result = append(result, "")
			}
		}

		indented := strings.Repeat("  ", depth) + trimmed
		result = append(result, indented)
		prevDepth = depth

		if strings.HasSuffix(trimmed, "{") {
			depth++
		}
	}

	out := strings.Join(result, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}
