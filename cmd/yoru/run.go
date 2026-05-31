package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adriangitvitz/yoru/agent"
	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/service"
	"github.com/adriangitvitz/yoru/stdlib"
	"github.com/adriangitvitz/yoru/typechecker"
)

func installLLMClient(interp *interpreter.Interpreter) {
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		interp.SetLLMClient(agent.NewOpenRouterClient())
		return
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		interp.SetLLMClient(agent.NewAnthropicClient())
		return
	}
}

func runCmd(filename string, scriptArgs []string) (exitCode int) {
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", filename, err)
		return 1
	}
	src := string(data)

	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			fmt.Fprintf(os.Stderr, "%s: parse error: %s\n", filename, e)
		}
		return 2
	}

	if !hasImports(prog) {
		c := typechecker.NewChecker()
		res := c.Check(prog)
		if len(res.Errors) > 0 {
			for _, e := range res.Errors {
				msg := strings.TrimPrefix(e, "parse error: ")
				fmt.Fprintf(os.Stderr, "%s: type error: %s\n", filename, msg)
			}
			return 2
		}
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", filename, r)
			exitCode = 3
		}
	}()

	interp := interpreter.NewInterpreter()
	stdlib.InstallAll(interp, os.Stderr)
	installLLMClient(interp)
	interp.SetScriptArgs(scriptArgs)

	interp.SetBaseDir(filepath.Dir(absPath(filename)))
	interp.SetFileReader(&osFileReader{})

	if hasImports(prog) {
		if err := interp.ProcessImports(prog); err != nil {
			fmt.Fprintf(os.Stderr, "%s: import error: %s\n", filename, err)
			return 2
		}
	}

	_, _ = interp.EvalProgram(prog)

	if hasFnMain(prog) {
		mainL := lexer.New("main()")
		mainP := parser.New(mainL)
		mainProg := mainP.ParseProgram()
		_, _ = interp.EvalProgram(mainProg)
	}

	serviceDecls := interp.GetServiceDecls()
	if len(serviceDecls) > 0 {
		var decl *parser.ServiceDecl
		for _, d := range serviceDecls {
			decl = d
			break
		}

		srv, err := service.NewServer(service.ServiceConfig{
			Decl:   decl,
			Interp: interp,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: service error: %s\n", filename, err)
			return 3
		}

		fmt.Fprintf(os.Stderr, "HTTP server listening on :%d\n", decl.Port)
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "%s: server error: %s\n", filename, err)
			return 3
		}
	}

	return 0
}

func hasFnMain(prog *parser.Program) bool {
	for _, stmt := range prog.Statements {
		if fn, ok := stmt.(*parser.FnDecl); ok && fn.Name == "main" {
			return true
		}
		if es, ok := stmt.(*parser.ExportStatement); ok {
			if fn, ok := es.Inner.(*parser.FnDecl); ok && fn.Name == "main" {
				return true
			}
		}
	}
	return false
}

func hasImports(prog *parser.Program) bool {
	for _, stmt := range prog.Statements {
		if _, ok := stmt.(*parser.ImportStatement); ok {
			return true
		}
	}
	return false
}

func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

type osFileReader struct{}

func (r *osFileReader) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *osFileReader) ListDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func (r *osFileReader) IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
