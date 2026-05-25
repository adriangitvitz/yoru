package interpreter

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
)

// FileReader abstracts filesystem access for testability.
type FileReader interface {
	ReadFile(path string) (string, error)
	ListDir(path string) ([]string, error)
	IsDir(path string) bool
}

// Module represents a resolved and evaluated import.
type Module struct {
	Env                *Environment
	ObjectDecls        map[string]*parser.ObjectDecl
	EnumDecls          map[string]*parser.EnumDecl
	FnDecls            map[string]*parser.FnDecl
	ActorDecls         map[string]*parser.ActorDecl
	HasExplicitExports bool
	ExportedNames      map[string]bool
}

// SetFileReader configures the filesystem backend for imports.
func (interp *Interpreter) SetFileReader(fr FileReader) {
	interp.fileReader = fr
}

// SetBaseDir sets the base directory for resolving import paths.
func (interp *Interpreter) SetBaseDir(dir string) {
	interp.baseDir = dir
}

// ProcessImports resolves all import statements in a program.
func (interp *Interpreter) ProcessImports(prog *parser.Program) error {
	for _, stmt := range prog.Statements {
		if imp, ok := stmt.(*parser.ImportStatement); ok {
			if err := interp.resolveImport(imp, interp.baseDir); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveImport memoizes by absolute path and breaks cycles via importStack.
func (interp *Interpreter) resolveImport(stmt *parser.ImportStatement, callerDir string) error {
	resolvedPath := stmt.Path
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(callerDir, resolvedPath)
	}
	resolvedPath = filepath.Clean(resolvedPath)

	if interp.fileReader.IsDir(resolvedPath) {
		return interp.resolveDirectoryImport(stmt, resolvedPath)
	}

	if !strings.HasSuffix(resolvedPath, ".yr") {
		resolvedPath += ".yr"
	}

	if mod, ok := interp.moduleCache[resolvedPath]; ok {
		interp.bindModule(mod, stmt)
		return nil
	}

	if slices.Contains(interp.importStack, resolvedPath) {
		cycle := append(interp.importStack, resolvedPath)
		return fmt.Errorf("import cycle detected: %s", strings.Join(cycle, " -> "))
	}

	interp.importStack = append(interp.importStack, resolvedPath)
	defer func() {
		interp.importStack = interp.importStack[:len(interp.importStack)-1]
	}()

	src, err := interp.fileReader.ReadFile(resolvedPath)
	if err != nil {
		return fmt.Errorf("import not found: %s", resolvedPath)
	}

	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return fmt.Errorf("parse error in %s: %s", resolvedPath, strings.Join(p.Errors(), "; "))
	}

	// Share moduleCache + importStack so cycle/memo spans the whole graph.
	child := NewInterpreter()
	child.fileReader = interp.fileReader
	child.baseDir = filepath.Dir(resolvedPath)
	child.moduleCache = interp.moduleCache
	child.importStack = interp.importStack

	if err := child.ProcessImports(prog); err != nil {
		return err
	}

	child.collectDeclarations(prog)
	for _, s := range prog.Statements {
		child.evalStatement(s)
	}

	mod := &Module{
		Env:                child.env,
		ObjectDecls:        child.objectDecls,
		EnumDecls:          child.enumDecls,
		FnDecls:            make(map[string]*parser.FnDecl),
		ActorDecls:         child.actorDecls,
		HasExplicitExports: child.hasExplicitExports,
		ExportedNames:      child.exportedNames,
	}

	interp.moduleCache[resolvedPath] = mod
	interp.bindModule(mod, stmt)
	return nil
}

// resolveDirectoryImport merges every .yr file in dirPath into the caller's
// scope (or under stmt.Alias). Each file still flows through resolveImport.
func (interp *Interpreter) resolveDirectoryImport(stmt *parser.ImportStatement, dirPath string) error {
	files, err := interp.fileReader.ListDir(dirPath)
	if err != nil {
		return fmt.Errorf("cannot read directory: %s", dirPath)
	}

	mergedEnv := NewEnvironment()
	mergedObjectDecls := make(map[string]*parser.ObjectDecl)
	mergedEnumDecls := make(map[string]*parser.EnumDecl)
	mergedActorDecls := make(map[string]*parser.ActorDecl)

	for _, fname := range files {
		if !strings.HasSuffix(fname, ".yr") {
			continue
		}
		filePath := filepath.Join(dirPath, fname)

		fileStmt := &parser.ImportStatement{
			Token: stmt.Token,
			Path:  filePath,
		}

		if err := interp.resolveImport(fileStmt, filepath.Dir(filePath)); err != nil {
			return err
		}

		if mod, ok := interp.moduleCache[filePath]; ok {
			for name, val := range mod.Env.Exports() {
				mergedEnv.Set(name, val)
			}
			maps.Copy(mergedObjectDecls, mod.ObjectDecls)
			maps.Copy(mergedEnumDecls, mod.EnumDecls)
			maps.Copy(mergedActorDecls, mod.ActorDecls)
		}
	}

	if stmt.Alias != "" {
		fields := make(map[string]Value)
		maps.Copy(fields, mergedEnv.Exports())
		interp.env.Set(stmt.Alias, &ObjectVal{
			TypeName: "__namespace__",
			Fields:   fields,
		})
	}

	return nil
}

// bindModule binds a resolved module's exports into the current interpreter scope.
func (interp *Interpreter) bindModule(mod *Module, stmt *parser.ImportStatement) {
	var exports map[string]Value
	if mod.HasExplicitExports {
		exports = mod.Env.ExportsFiltered(mod.ExportedNames)
	} else {
		exports = mod.Env.Exports()
	}

	// '_'-prefixed names are private; always skipped.
	switch {
	case stmt.Alias != "":
		// import "./b" as B
		fields := make(map[string]Value)
		maps.Copy(fields, exports)
		interp.env.Set(stmt.Alias, &ObjectVal{
			TypeName: "__namespace__",
			Fields:   fields,
		})
		for name, decl := range mod.ObjectDecls {
			if !strings.HasPrefix(name, "_") {
				interp.objectDecls[stmt.Alias+"."+name] = decl
			}
		}
		for name, decl := range mod.EnumDecls {
			if !strings.HasPrefix(name, "_") {
				interp.enumDecls[stmt.Alias+"."+name] = decl
			}
		}

	case len(stmt.Names) > 0:
		// import { a, b } from "./path"
		for _, name := range stmt.Names {
			if strings.HasPrefix(name, "_") {
				continue
			}
			if val, ok := exports[name]; ok {
				interp.env.Set(name, val)
			}
		}
		for _, name := range stmt.Names {
			if decl, ok := mod.ObjectDecls[name]; ok {
				interp.objectDecls[name] = decl
			}
			if decl, ok := mod.EnumDecls[name]; ok {
				interp.enumDecls[name] = decl
			}
		}

	default:
		// import "./b" — merge all exports flat
		for name, val := range exports {
			interp.env.Set(name, val)
		}
		for name, decl := range mod.ObjectDecls {
			if !strings.HasPrefix(name, "_") {
				interp.objectDecls[name] = decl
			}
		}
		for name, decl := range mod.EnumDecls {
			if !strings.HasPrefix(name, "_") {
				interp.enumDecls[name] = decl
			}
		}
		for name, decl := range mod.ActorDecls {
			if !strings.HasPrefix(name, "_") {
				interp.actorDecls[name] = decl
			}
		}
	}
}
