package stdlib

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adriangitvitz/yoru/interpreter"
)

// PathProvider implements the Path effect namespace.
type PathProvider struct{}

func (p *PathProvider) EffectName() string { return "Path" }

func (p *PathProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"join":      builtin("Path.join", p.join),
		"resolve":   builtin("Path.resolve", p.resolve),
		"dirname":   builtin("Path.dirname", p.dirname),
		"basename":  builtin("Path.basename", p.basename),
		"extname":   builtin("Path.extname", p.extname),
		"is_within": builtin("Path.is_within", p.isWithin),
	}
}

func (p *PathProvider) join(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 1 {
		return pathErr("path_bad_args", "Path.join(parts) takes 1 argument"), nil
	}
	list, ok := args[0].(*interpreter.ListVal)
	if !ok {
		return pathErr("path_bad_args", "Path.join argument must be a list of strings"), nil
	}
	parts := make([]string, 0, len(list.Elements))
	for i, e := range list.Elements {
		s, ok := e.(*interpreter.StringVal)
		if !ok {
			return pathErr("path_bad_args", fmt.Sprintf("Path.join element %d must be a String", i)), nil
		}
		parts = append(parts, s.V)
	}
	return &interpreter.StringVal{V: filepath.Join(parts...)}, nil
}

func (p *PathProvider) resolve(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "Path.resolve(path)")
	if err != nil {
		return pathErr("path_bad_args", err.Error()), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return pathErr("path_io", err.Error()), nil
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return &interpreter.StringVal{V: abs}, nil
}

func (p *PathProvider) dirname(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "Path.dirname(path)")
	if err != nil {
		return pathErr("path_bad_args", err.Error()), nil
	}
	return &interpreter.StringVal{V: filepath.Dir(path)}, nil
}

func (p *PathProvider) basename(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "Path.basename(path)")
	if err != nil {
		return pathErr("path_bad_args", err.Error()), nil
	}
	return &interpreter.StringVal{V: filepath.Base(path)}, nil
}

func (p *PathProvider) extname(args []interpreter.Value) (interpreter.Value, error) {
	path, err := strArg(args, 0, "Path.extname(path)")
	if err != nil {
		return pathErr("path_bad_args", err.Error()), nil
	}
	return &interpreter.StringVal{V: filepath.Ext(path)}, nil
}

// isWithin returns true when child resolves to a path equal to or
// nested under parent (both with symlinks and `..` resolved).
func (p *PathProvider) isWithin(args []interpreter.Value) (interpreter.Value, error) {
	child, parent, err := twoStringArgs(args, "Path.is_within(child, parent)")
	if err != nil {
		return pathErr("path_bad_args", err.Error()), nil
	}
	c, errC := resolveForCompare(child)
	pa, errP := resolveForCompare(parent)
	if errC != nil || errP != nil {
		return &interpreter.BoolVal{V: false}, nil
	}
	rel, err := filepath.Rel(pa, c)
	if err != nil {
		return &interpreter.BoolVal{V: false}, nil
	}
	if rel == "." {
		return &interpreter.BoolVal{V: true}, nil
	}
	if strings.HasPrefix(rel, "..") {
		return &interpreter.BoolVal{V: false}, nil
	}
	return &interpreter.BoolVal{V: true}, nil
}

// resolveForCompare returns an absolute, symlink-resolved path. When the
// target doesn't exist, it walks up to the longest existing ancestor,
// resolves that, and reattaches the missing tail so sandbox checks work
// on systems where a parent is itself a symlink (e.g. macOS /tmp).
func resolveForCompare(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	current := abs
	var tail []string
	for {
		if _, err := os.Stat(current); err == nil {
			real, err := filepath.EvalSymlinks(current)
			if err != nil {
				return abs, nil
			}
			parts := append([]string{real}, tail...)
			return filepath.Join(parts...), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		tail = append([]string{filepath.Base(current)}, tail...)
		current = parent
	}
}

func pathErr(kind, message string) interpreter.Value {
	return &interpreter.EnumVal{
		TypeName: "Result",
		Variant:  "Err",
		Fields: map[string]interpreter.Value{
			"error": &interpreter.ObjectVal{
				TypeName: "Error",
				Fields: map[string]interpreter.Value{
					"kind":    &interpreter.StringVal{V: kind},
					"message": &interpreter.StringVal{V: message},
				},
			},
		},
	}
}
