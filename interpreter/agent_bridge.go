package interpreter

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adriangitvitz/yoru/agent"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/tool"
)

// ChatTimeout bounds how long agent_ref.chat(prompt) will wait for a reply.
var ChatTimeout = 120 * time.Second

func (interp *Interpreter) SetLLMClient(client agent.LLMClient) {
	interp.llmClient = client
}

func (interp *Interpreter) evalAgentChat(ref *ActorRef, callArgs []parser.CallArg) Value {
	if len(callArgs) == 0 {
		return makeErrResult("chat_bad_args", "chat() requires a prompt argument")
	}
	val := interp.evalExpression(callArgs[0].Value)
	prompt, ok := val.(*StringVal)
	if !ok {
		return makeErrResult("chat_bad_args", "chat() prompt must be a String")
	}

	replyCh := make(chan Value, 1)
	ref.Mailbox <- ActorMessage{
		Method:  "chat",
		Args:    map[string]Value{"prompt": prompt},
		ReplyCh: replyCh,
	}
	select {
	case result := <-replyCh:
		if ev, isResult := result.(*EnumVal); isResult && ev.TypeName == "Result" {
			return ev
		}
		return makeOkResult(result)
	case <-time.After(ChatTimeout):
		return makeErrResult("chat_timeout", "agent chat exceeded "+ChatTimeout.String())
	}
}

func (interp *Interpreter) spawnAgent(decl *parser.AgentDecl) Value {
	return interp.spawnAgentWithCrash(decl, nil)
}

func (interp *Interpreter) spawnSupervisedAgent(decl *parser.AgentDecl, onCrash func(ref *ActorRef, reason string)) *ActorRef {
	v := interp.spawnAgentWithCrash(decl, onCrash)
	if ref, ok := v.(*ActorRef); ok {
		return ref
	}
	return nil
}

func (interp *Interpreter) spawnAgentWithCrash(decl *parser.AgentDecl, onCrash func(ref *ActorRef, reason string)) Value {
	if interp.llmClient == nil {
		return spawnDeadAgent(decl.Name)
	}

	reg := tool.NewRegistry()
	for _, toolName := range decl.Tools {
		schema := interp.GetToolSchema(toolName)
		if schema == nil {
			continue
		}
		name := toolName
		_ = reg.Register(schema, func(args json.RawMessage) (string, error) {
			return interp.InvokeToolJSON(name, args)
		})
	}

	toolSchemas := make([]*tool.ToolSchema, 0, len(decl.Tools))
	for _, toolName := range decl.Tools {
		if schema := interp.GetToolSchema(toolName); schema != nil {
			toolSchemas = append(toolSchemas, schema)
		}
	}

	var outputSchema *agent.OutputSchema
	if len(decl.Outputs) > 0 {
		props := make(map[string]agent.OutputProperty, len(decl.Outputs))
		var required []string
		for _, f := range decl.Outputs {
			prop := agent.OutputProperty{Type: yoruTypeToJSONType(f.TypeExpr)}
			if f.Annotation != nil && f.Annotation.Name == "doc" && len(f.Annotation.Args) > 0 {
				if sl, ok := f.Annotation.Args[0].(*parser.StringLiteral); ok {
					prop.Description = sl.Value
				}
			}
			props[f.Name] = prop
			required = append(required, f.Name)
		}
		outputSchema = &agent.OutputSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		}
	}

	declaredSet := make(map[string]struct{}, len(decl.Tools))
	for _, name := range decl.Tools {
		declaredSet[name] = struct{}{}
	}

	refresh := func() []*tool.ToolSchema {
		out := make([]*tool.ToolSchema, 0, len(decl.Tools))
		for _, name := range decl.Tools {
			if s := interp.GetToolSchema(name); s != nil {
				out = append(out, s)
			}
		}
		for name := range interp.toolDecls {
			if _, declared := declaredSet[name]; declared {
				continue
			}
			s := interp.GetToolSchema(name)
			if s == nil {
				continue
			}
			out = append(out, s)
			capturedName := name
			_ = reg.Register(s, func(args json.RawMessage) (string, error) {
				return interp.InvokeToolJSON(capturedName, args)
			})
		}
		return out
	}

	ag := agent.NewAgent(agent.AgentConfig{
		Model:              decl.Model,
		System:             decl.System,
		Tools:              toolSchemas,
		MaxTurns:           decl.MaxTurns,
		BudgetTokens:       decl.BudgetTokens,
		Temperature:        decl.Temperature,
		OutputSchema:       outputSchema,
		RetryInvalidOutput: decl.RetryInvalidOutput,
		RefreshTools:       refresh,
	}, interp.llmClient, reg)
	if os.Getenv("YORU_AGENT_DEBUG") != "" {
		ag.Debug = true
	}

	mailbox := make(chan ActorMessage, 256)
	done := make(chan struct{})

	ref := &ActorRef{
		Name:    decl.Name,
		Mailbox: mailbox,
		Done:    done,
	}

	go runAgent(ag, ref, mailbox, done, onCrash, decl.Name, decl.Outputs)
	return ref
}

func yoruTypeToJSONType(yoruType string) string {
	switch yoruType {
	case "Int":
		return "integer"
	case "Float":
		return "number"
	case "Bool":
		return "boolean"
	case "String":
		return "string"
	}
	return "string"
}

func spawnDeadAgent(name string) *ActorRef {
	mailbox := make(chan ActorMessage, 64)
	done := make(chan struct{})
	ref := &ActorRef{Name: name, Mailbox: mailbox, Done: done}

	go func() {
		defer close(done)
		for msg := range mailbox {
			if msg.ReplyCh == nil {
				continue
			}
			msg.ReplyCh <- makeErrResult("llm_not_configured",
				"no LLMClient installed on the interpreter; call SetLLMClient or handle(LLM) first")
		}
	}()

	return ref
}

func runAgent(ag *agent.Agent, ref *ActorRef, mailbox chan ActorMessage, done chan struct{}, onCrash func(*ActorRef, string), agentName string, outputs []parser.Field) {
	defer func() {
		r := recover()
		close(done)
		if r != nil && onCrash != nil {
			go onCrash(ref, fmt.Sprintf("%v", r))
		}
	}()
	for msg := range mailbox {
		if msg.Method != "chat" || msg.ReplyCh == nil {
			continue
		}
		prompt, _ := msg.Args["prompt"].(*StringVal)
		var promptStr string
		if prompt != nil {
			promptStr = prompt.V
		}
		reply, err := ag.Run(promptStr)
		if err != nil {
			if strings.Contains(err.Error(), "agent_output_invalid") {
				msg.ReplyCh <- makeErrResult("agent_output_invalid", err.Error())
			} else {
				msg.ReplyCh <- makeErrResult("agent_error", err.Error())
			}
			continue
		}
		if len(outputs) > 0 {
			obj, parseErr := jsonToOutputObject(reply, agentName, outputs)
			if parseErr != nil {
				msg.ReplyCh <- makeErrResult("agent_output_invalid",
					"agent reply could not be re-tagged as "+agentName+".Output: "+parseErr.Error())
				continue
			}
			msg.ReplyCh <- obj
			continue
		}
		msg.ReplyCh <- &StringVal{V: reply}
	}
}

func jsonToOutputObject(jsonText, agentName string, _ []parser.Field) (*ObjectVal, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		return nil, err
	}
	fields := make(map[string]Value, len(raw))
	for k, v := range raw {
		fields[k] = jsonValueToYoruValue(v)
	}
	return &ObjectVal{TypeName: agentName + ".Output", Fields: fields}, nil
}

func jsonValueToYoruValue(v any) Value {
	switch x := v.(type) {
	case string:
		return &StringVal{V: x}
	case float64:
		if x == float64(int64(x)) {
			return &IntVal{V: int64(x)}
		}
		return &FloatVal{V: x}
	case bool:
		return &BoolVal{V: x}
	case nil:
		return &NilVal{}
	case []any:
		elems := make([]Value, len(x))
		for i, e := range x {
			elems[i] = jsonValueToYoruValue(e)
		}
		return &ListVal{Elements: elems}
	case map[string]any:
		f := make(map[string]Value, len(x))
		for k, v := range x {
			f[k] = jsonValueToYoruValue(v)
		}
		return &ObjectVal{TypeName: "Object", Fields: f}
	}
	return &NilVal{}
}
