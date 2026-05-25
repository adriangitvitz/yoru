package stdlib

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
)

// SubprocessProvider implements Subprocess.run(cmd, args, stdin?, timeout_ms?).
// Returns {stdout, stderr, exit_code} on completion (any exit code),
// Result.Err on spawn/IO failure or timeout. No shell: args go straight to
// execve. cmd is resolved via PATH; absolute paths are honored as-is.
type SubprocessProvider struct {
	// DefaultTimeout caps each call; keeps stray subprocesses from hanging.
	DefaultTimeout time.Duration
	// MaxOutputBytes caps stdout/stderr; truncation appends "...[truncated]".
	MaxOutputBytes int
}

// NewSubprocessProvider returns a provider with a 60s timeout and 16 MiB cap.
func NewSubprocessProvider() *SubprocessProvider {
	return &SubprocessProvider{
		DefaultTimeout: 60 * time.Second,
		MaxOutputBytes: 16 * 1024 * 1024,
	}
}

func (p *SubprocessProvider) EffectName() string { return "Subprocess" }

func (p *SubprocessProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"run": &interpreter.BuiltinVal{
			Name: "Subprocess.run",
			Fn: func(args []interpreter.Value) (interpreter.Value, error) {
				if len(args) < 2 {
					return subprocessErr("subprocess_bad_args",
						"Subprocess.run(cmd, args, stdin?, timeout_ms?) requires at least cmd and args"), nil
				}
				cmdStr, ok := args[0].(*interpreter.StringVal)
				if !ok {
					return subprocessErr("subprocess_bad_args", "cmd must be a String"), nil
				}
				argList, ok := args[1].(*interpreter.ListVal)
				if !ok {
					return subprocessErr("subprocess_bad_args", "args must be a List of String"), nil
				}
				argStrs := make([]string, 0, len(argList.Elements))
				for _, el := range argList.Elements {
					s, ok := el.(*interpreter.StringVal)
					if !ok {
						return subprocessErr("subprocess_bad_args", "every arg must be a String"), nil
					}
					argStrs = append(argStrs, s.V)
				}
				stdin := ""
				if len(args) >= 3 {
					if s, ok := args[2].(*interpreter.StringVal); ok {
						stdin = s.V
					} else if _, ok := args[2].(*interpreter.NilVal); !ok {
						return subprocessErr("subprocess_bad_args", "stdin must be a String or nil"), nil
					}
				}
				timeout := p.DefaultTimeout
				if len(args) >= 4 {
					if n, ok := args[3].(*interpreter.IntVal); ok {
						timeout = time.Duration(n.V) * time.Millisecond
					}
				}
				return p.exec(cmdStr.V, argStrs, stdin, timeout), nil
			},
		},
	}
}

func (p *SubprocessProvider) exec(cmd string, args []string, stdin string, timeout time.Duration) interpreter.Value {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := exec.CommandContext(ctx, cmd, args...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return subprocessErr("subprocess_timeout",
			fmt.Sprintf("%s timed out after %s", cmd, timeout))
	}

	stdoutStr := capOutput(stdout.String(), p.MaxOutputBytes)
	stderrStr := capOutput(stderr.String(), p.MaxOutputBytes)

	exitCode := int64(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int64(exitErr.ExitCode())
		} else {
			// Spawn failure (not-found, permission-denied): hard error.
			return subprocessErr("subprocess_spawn_failed", err.Error())
		}
	}

	return &interpreter.ObjectVal{
		TypeName: "SubprocessResult",
		Fields: map[string]interpreter.Value{
			"stdout":    &interpreter.StringVal{V: stdoutStr},
			"stderr":    &interpreter.StringVal{V: stderrStr},
			"exit_code": &interpreter.IntVal{V: exitCode},
		},
	}
}

func capOutput(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]"
}

func subprocessErr(kind, message string) interpreter.Value {
	errObj := &interpreter.ObjectVal{
		TypeName: "Error",
		Fields: map[string]interpreter.Value{
			"kind":    &interpreter.StringVal{V: kind},
			"message": &interpreter.StringVal{V: message},
		},
	}
	return &interpreter.EnumVal{
		TypeName: "Result",
		Variant:  "Err",
		Fields: map[string]interpreter.Value{
			"error": errObj,
		},
	}
}
