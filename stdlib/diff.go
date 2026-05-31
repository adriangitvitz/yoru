package stdlib

import (
	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/pmezard/go-difflib/difflib"
)

// DiffProvider implements the Diff effect namespace, backed by go-difflib.
type DiffProvider struct{}

func (p *DiffProvider) EffectName() string { return "Diff" }

func (p *DiffProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"unified":       builtin("Diff.unified", p.unified),
		"unified_named": builtin("Diff.unified_named", p.unifiedNamed),
	}
}

func (p *DiffProvider) unified(args []interpreter.Value) (interpreter.Value, error) {
	a, b, err := twoStringArgs(args, "Diff.unified(a, b)")
	if err != nil {
		return diffErr("diff_bad_args", err.Error()), nil
	}
	out, derr := renderUnified(a, b, "file")
	if derr != nil {
		return diffErr("diff_internal", derr.Error()), nil
	}
	return &interpreter.StringVal{V: out}, nil
}

func (p *DiffProvider) unifiedNamed(args []interpreter.Value) (interpreter.Value, error) {
	if len(args) != 3 {
		return diffErr("diff_bad_args", "Diff.unified_named(a, b, path) takes 3 arguments"), nil
	}
	a, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return diffErr("diff_bad_args", "a must be a String"), nil
	}
	b, ok := args[1].(*interpreter.StringVal)
	if !ok {
		return diffErr("diff_bad_args", "b must be a String"), nil
	}
	path, ok := args[2].(*interpreter.StringVal)
	if !ok {
		return diffErr("diff_bad_args", "path must be a String"), nil
	}
	out, err := renderUnified(a.V, b.V, path.V)
	if err != nil {
		return diffErr("diff_internal", err.Error()), nil
	}
	return &interpreter.StringVal{V: out}, nil
}

func renderUnified(a, b, path string) (string, error) {
	return difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(a),
		B:        difflib.SplitLines(b),
		FromFile: "a/" + path,
		ToFile:   "b/" + path,
		Context:  3,
	})
}

func diffErr(kind, message string) interpreter.Value {
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
