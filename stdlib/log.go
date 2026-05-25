package stdlib

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
)

// LogLevel controls which log levels are emitted.
type LogLevel int

const (
	LogDebug LogLevel = iota
	LogInfo
	LogWarn
	LogError
)

// LogProvider implements the Log effect namespace.
// Backed by encoding/json + os + time + sync from Go stdlib.
type LogProvider struct {
	writer io.Writer
	level  LogLevel
	mu     sync.Mutex
	ctx    map[string]string
}

func NewLogProvider(writer io.Writer, level LogLevel) *LogProvider {
	if writer == nil {
		writer = os.Stderr
	}
	return &LogProvider{
		writer: writer,
		level:  level,
		ctx:    make(map[string]string),
	}
}

func (p *LogProvider) EffectName() string { return "Log" }

func (p *LogProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"debug": &interpreter.BuiltinVal{Name: "Log.debug", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("Log.debug() takes 1 argument")
			}
			p.log("debug", args[0].(*interpreter.StringVal).V, nil)
			return &interpreter.NilVal{}, nil
		}},
		"info": &interpreter.BuiltinVal{Name: "Log.info", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("Log.info() takes 1 argument")
			}
			p.log("info", args[0].(*interpreter.StringVal).V, nil)
			return &interpreter.NilVal{}, nil
		}},
		"warn": &interpreter.BuiltinVal{Name: "Log.warn", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("Log.warn() takes 1 argument")
			}
			p.log("warn", args[0].(*interpreter.StringVal).V, nil)
			return &interpreter.NilVal{}, nil
		}},
		"error": &interpreter.BuiltinVal{Name: "Log.error", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("Log.error() takes 1 argument")
			}
			p.log("error", args[0].(*interpreter.StringVal).V, nil)
			return &interpreter.NilVal{}, nil
		}},
		"with": &interpreter.BuiltinVal{Name: "Log.with", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("Log.with() takes 1 argument")
			}
			obj, ok := args[0].(*interpreter.ObjectVal)
			if !ok {
				return nil, fmt.Errorf("Log.with() argument must be an Object")
			}
			p.mu.Lock()
			for k, v := range obj.Fields {
				if sv, ok := v.(*interpreter.StringVal); ok {
					p.ctx[k] = sv.V
				}
			}
			p.mu.Unlock()
			return &interpreter.NilVal{}, nil
		}},
		"info_fields": &interpreter.BuiltinVal{Name: "Log.info_fields", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("Log.info_fields() takes 2 arguments")
			}
			msg := args[0].(*interpreter.StringVal).V
			obj, ok := args[1].(*interpreter.ObjectVal)
			if !ok {
				return nil, fmt.Errorf("Log.info_fields() second argument must be an Object")
			}
			fields := make(map[string]string)
			for k, v := range obj.Fields {
				if sv, ok := v.(*interpreter.StringVal); ok {
					fields[k] = sv.V
				}
			}
			p.log("info", msg, fields)
			return &interpreter.NilVal{}, nil
		}},
		"error_fields": &interpreter.BuiltinVal{Name: "Log.error_fields", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("Log.error_fields() takes 2 arguments")
			}
			msg := args[0].(*interpreter.StringVal).V
			obj, ok := args[1].(*interpreter.ObjectVal)
			if !ok {
				return nil, fmt.Errorf("Log.error_fields() second argument must be an Object")
			}
			fields := make(map[string]string)
			for k, v := range obj.Fields {
				if sv, ok := v.(*interpreter.StringVal); ok {
					fields[k] = sv.V
				}
			}
			p.log("error", msg, fields)
			return &interpreter.NilVal{}, nil
		}},
	}
}

func (p *LogProvider) log(level, msg string, oneShot map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lvl LogLevel
	switch level {
	case "debug":
		lvl = LogDebug
	case "info":
		lvl = LogInfo
	case "warn":
		lvl = LogWarn
	case "error":
		lvl = LogError
	}
	if lvl < p.level {
		return
	}

	entry := map[string]string{
		"level": level,
		"msg":   msg,
		"ts":    time.Now().UTC().Format(time.RFC3339),
	}
	maps.Copy(entry, p.ctx)
	// One-shot fields are not persisted into p.ctx.
	maps.Copy(entry, oneShot)

	b, _ := json.Marshal(entry)
	_, _ = fmt.Fprintln(p.writer, string(b))
}
