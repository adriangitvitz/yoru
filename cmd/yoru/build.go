package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
)

func buildCmd(args []string) int {
	var target, output, sourceFile string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 < len(args) {
				i++
				target = args[i]
			}
		case "--output":
			if i+1 < len(args) {
				i++
				output = args[i]
			}
		default:
			if !strings.HasPrefix(args[i], "-") {
				sourceFile = args[i]
			}
		}
	}

	if target != "mcp" && target != "http" && target != "cli" {
		fmt.Fprintln(os.Stderr, "usage: yoru build --target <mcp|http|cli> [--output <path>] <source.yr>")
		return 1
	}

	if sourceFile == "" {
		fmt.Fprintln(os.Stderr, "usage: yoru build --target <mcp|http|cli> [--output <path>] <source.yr>")
		return 1
	}

	data, err := os.ReadFile(sourceFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %s\n", sourceFile, err)
		return 1
	}
	src := string(data)

	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			fmt.Fprintf(os.Stderr, "%s: parse error: %s\n", sourceFile, e)
		}
		return 2
	}

	interp := interpreter.NewInterpreter()
	_, err = interp.EvalProgram(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", sourceFile, err)
		return 2
	}

	switch target {
	case "mcp":
		return buildMCP(interp, src, sourceFile, output)
	case "http":
		return buildHTTP(interp, src, sourceFile, output)
	case "cli":
		return buildCLI(src, sourceFile, output)
	}

	return 1
}

func buildCLI(src, sourceFile, output string) int {
	if output == "" {
		base := filepath.Base(sourceFile)
		ext := filepath.Ext(base)
		output = strings.TrimSuffix(base, ext)
	}
	templateData := struct {
		Source string
	}{
		Source: escapeBackticks(src),
	}
	return generateAndBuild(cliMainTemplate, templateData, "yoru-cli-build-*", output, "CLI binary")
}

func buildMCP(interp *interpreter.Interpreter, src, sourceFile, output string) int {
	mcpDecls := interp.GetMCPDecls()
	if len(mcpDecls) == 0 {
		fmt.Fprintln(os.Stderr, "error: no mcp declaration found in source")
		return 2
	}
	if len(mcpDecls) > 1 {
		fmt.Fprintln(os.Stderr, "error: multiple mcp declarations found; exactly one expected")
		return 2
	}

	var mcpDecl *parser.MCPDecl
	for _, d := range mcpDecls {
		mcpDecl = d
		break
	}

	if output == "" {
		base := filepath.Base(sourceFile)
		ext := filepath.Ext(base)
		output = strings.TrimSuffix(base, ext)
	}

	templateData := struct {
		Source     string
		ServerName string
		Version    string
		Tools      []string
	}{
		Source:     escapeBackticks(src),
		ServerName: mcpDecl.ServerName,
		Version:    mcpDecl.Version,
		Tools:      mcpDecl.Tools,
	}

	return generateAndBuild(mcpMainTemplate, templateData, "yoru-mcp-build-*", output, "MCP server")
}

func buildHTTP(interp *interpreter.Interpreter, src, sourceFile, output string) int {
	serviceDecls := interp.GetServiceDecls()
	if len(serviceDecls) == 0 {
		fmt.Fprintln(os.Stderr, "error: no service declaration found in source")
		return 2
	}
	if len(serviceDecls) > 1 {
		fmt.Fprintln(os.Stderr, "error: multiple service declarations found; exactly one expected")
		return 2
	}

	var serviceDecl *parser.ServiceDecl
	for _, d := range serviceDecls {
		serviceDecl = d
		break
	}

	env := interp.Env()
	for _, route := range serviceDecl.Routes {
		if _, ok := env.Get(route.Handler); !ok {
			fmt.Fprintf(os.Stderr, "error: service '%s' references unknown handler '%s'\n", serviceDecl.Name, route.Handler)
			return 2
		}
	}

	if output == "" {
		base := filepath.Base(sourceFile)
		ext := filepath.Ext(base)
		output = strings.TrimSuffix(base, ext)
	}

	templateData := struct {
		Source string
		Port   int
	}{
		Source: escapeBackticks(src),
		Port:   serviceDecl.Port,
	}

	return generateAndBuild(httpMainTemplate, templateData, "yoru-http-build-*", output, "HTTP server")
}

func escapeBackticks(s string) string {
	if !strings.Contains(s, "`") {
		return s
	}
	return strings.ReplaceAll(s, "`", "`+\"`\"+`")
}

func generateAndBuild(tmplStr string, data any, tmpPrefix, output, label string) int {
	yoruModPath := findYoruModPath()
	if yoruModPath == "" {
		fmt.Fprintln(os.Stderr, "error: cannot locate yoru module root")
		return 1
	}

	tmpDir, err := os.MkdirTemp("", tmpPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating temp dir: %s\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	goModContent := fmt.Sprintf(`module yoru-generated-server

go 1.25.1

require github.com/adriangitvitz/yoru v0.0.0

replace github.com/adriangitvitz/yoru => %s
`, yoruModPath)

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing go.mod: %s\n", err)
		return 1
	}

	tmpl, err := template.New("main").Parse(tmplStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing template: %s\n", err)
		return 1
	}

	mainFile, err := os.Create(filepath.Join(tmpDir, "main.go"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating main.go: %s\n", err)
		return 1
	}

	if err := tmpl.Execute(mainFile, data); err != nil {
		_ = mainFile.Close()
		fmt.Fprintf(os.Stderr, "error generating main.go: %s\n", err)
		return 1
	}
	_ = mainFile.Close()

	if !filepath.IsAbs(output) {
		cwd, _ := os.Getwd()
		output = filepath.Join(cwd, output)
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = tmpDir
	tidy.Stderr = os.Stderr
	if err := tidy.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "go mod tidy failed: %s\n", err)
		return 1
	}

	cmd := exec.Command("go", "build", "-o", output, ".")
	cmd.Dir = tmpDir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "go build failed: %s\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "Built %s: %s\n", label, output)
	return 0
}

func findYoruModPath() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		for range 5 {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir
			}
			dir = filepath.Dir(dir)
		}
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(homeDir(), "go")
	}
	modPath := filepath.Join(gopath, "src", "github.com", "adriangitvitz", "yoru")
	if _, err := os.Stat(filepath.Join(modPath, "go.mod")); err == nil {
		return modPath
	}

	cwd, _ := os.Getwd()
	dir := cwd
	for range 10 {
		goMod := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			data, err := os.ReadFile(goMod)
			if err == nil && strings.Contains(string(data), "github.com/adriangitvitz/yoru") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

func homeDir() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("USERPROFILE")
	}
	return os.Getenv("HOME")
}

const mcpMainTemplate = `package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/mcp"
	"github.com/adriangitvitz/yoru/stdlib"
	"github.com/adriangitvitz/yoru/tool"
)

const yoruSource = ` + "`" + `{{.Source}}` + "`" + `

func main() {
	interp := interpreter.NewInterpreter()
	stdlib.InstallAll(interp, os.Stderr)
	_, err := interp.EvalSourceInto(yoruSource)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	registry := tool.NewRegistry()
	{{range .Tools}}
	{
		name := "{{.}}"
		schema := interp.GetToolSchema(name)
		if schema != nil {
			registry.Register(schema, func(args json.RawMessage) (string, error) {
				return interp.InvokeToolJSON(name, args)
			})
		}
	}
	{{end}}

	server := mcp.NewServer(mcp.ServerConfig{
		Name:    "{{.ServerName}}",
		Version: "{{.Version}}",
		Tools:   registry,
	})

	if err := server.ServeStdio(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

const httpMainTemplate = `package main

import (
	"fmt"
	"os"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/service"
	"github.com/adriangitvitz/yoru/stdlib"
	yoruLexer "github.com/adriangitvitz/yoru/lexer"
)

const yoruSource = ` + "`" + `{{.Source}}` + "`" + `

func main() {
	l := yoruLexer.New(yoruSource)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			fmt.Fprintf(os.Stderr, "parse error: %s\n", e)
		}
		os.Exit(1)
	}

	interp := interpreter.NewInterpreter()
	stdlib.InstallAll(interp, os.Stderr)
	_, err := interp.EvalProgram(prog)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	serviceDecls := interp.GetServiceDecls()
	if len(serviceDecls) == 0 {
		fmt.Fprintln(os.Stderr, "error: no service declaration found")
		os.Exit(1)
	}

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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "HTTP server listening on :%d\n", {{.Port}})
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

const cliMainTemplate = `package main

import (
	"fmt"
	"os"

	"github.com/adriangitvitz/yoru/agent"
	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/stdlib"
	yoruLexer "github.com/adriangitvitz/yoru/lexer"
)

const yoruSource = ` + "`" + `{{.Source}}` + "`" + `

func main() {
	l := yoruLexer.New(yoruSource)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			fmt.Fprintf(os.Stderr, "parse error: %s\n", e)
		}
		os.Exit(1)
	}

	interp := interpreter.NewInterpreter()
	stdlib.InstallAll(interp, os.Stderr)

	if os.Getenv("OPENROUTER_API_KEY") != "" {
		interp.SetLLMClient(agent.NewOpenRouterClient())
	} else if os.Getenv("ANTHROPIC_API_KEY") != "" {
		interp.SetLLMClient(agent.NewAnthropicClient())
	}

	interp.SetScriptArgs(os.Args[1:])

	_, err := interp.EvalProgram(prog)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, stmt := range prog.Statements {
		fn, ok := stmt.(*parser.FnDecl)
		if !ok || fn.Name != "main" {
			continue
		}
		mainL := yoruLexer.New("main()")
		mainP := parser.New(mainL)
		mainProg := mainP.ParseProgram()
		_, _ = interp.EvalProgram(mainProg)
		return
	}
}
`
