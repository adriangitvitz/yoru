package stdlib

import (
	"fmt"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
)

// TimeProvider implements the Time effect.
type TimeProvider struct{}

func (p *TimeProvider) EffectName() string { return "Time" }

func (p *TimeProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"now_unix": &interpreter.BuiltinVal{Name: "Time.now_unix", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			return &interpreter.IntVal{V: time.Now().Unix()}, nil
		}},
		"now_ms": &interpreter.BuiltinVal{Name: "Time.now_ms", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			return &interpreter.IntVal{V: time.Now().UnixMilli()}, nil
		}},
		"now_iso": &interpreter.BuiltinVal{Name: "Time.now_iso", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			return &interpreter.StringVal{V: time.Now().UTC().Format(time.RFC3339)}, nil
		}},
		"sleep": &interpreter.BuiltinVal{Name: "Time.sleep", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("Time.sleep() takes 1 argument (ms)")
			}
			ms, ok := args[0].(*interpreter.IntVal)
			if !ok {
				return nil, fmt.Errorf("Time.sleep() argument must be Int")
			}
			time.Sleep(time.Duration(ms.V) * time.Millisecond)
			return &interpreter.NilVal{}, nil
		}},
		"add": &interpreter.BuiltinVal{Name: "Time.add", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("Time.add() takes 2 arguments (unix, seconds)")
			}
			unix, ok := args[0].(*interpreter.IntVal)
			if !ok {
				return nil, fmt.Errorf("Time.add() first argument must be Int")
			}
			seconds, ok := args[1].(*interpreter.IntVal)
			if !ok {
				return nil, fmt.Errorf("Time.add() second argument must be Int")
			}
			return &interpreter.IntVal{V: unix.V + seconds.V}, nil
		}},
		"format": &interpreter.BuiltinVal{Name: "Time.format", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("Time.format() takes 2 arguments (unix, layout)")
			}
			unix, ok := args[0].(*interpreter.IntVal)
			if !ok {
				return nil, fmt.Errorf("Time.format() first argument must be Int")
			}
			layout, ok := args[1].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Time.format() second argument must be String")
			}
			t := time.Unix(unix.V, 0).UTC()
			return &interpreter.StringVal{V: t.Format(layout.V)}, nil
		}},
		"parse": &interpreter.BuiltinVal{Name: "Time.parse", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("Time.parse() takes 2 arguments (s, layout)")
			}
			s, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Time.parse() first argument must be String")
			}
			layout, ok := args[1].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Time.parse() second argument must be String")
			}
			t, err := time.Parse(layout.V, s.V)
			if err != nil {
				return &interpreter.IntVal{V: 0}, nil
			}
			return &interpreter.IntVal{V: t.Unix()}, nil
		}},
	}
}
