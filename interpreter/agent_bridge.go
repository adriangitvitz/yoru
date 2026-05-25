package interpreter

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adriangitvitz/yoru/agent"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/tool"
)

// ChatTimeout bounds how long agent_ref.chat(prompt) will wait for a reply.
// Larger than AskTimeout because multi-turn LLM tool loops are slow.
var ChatTimeout = 120 * time.Second

// SetLLMClient injects the LLMClient used by agents spawned by this interpreter.
func (interp *Interpreter) SetLLMClient(client agent.LLMClient) {
	interp.llmClient = client
}

// evalAgentChat blocks until the agent replies; bounded by ChatTimeout.
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
		// Wrap success in Result.Ok to match .ask's shape. Pass an existing
		// Result through (don't double-wrap llm_not_configured/agent_error).
		if ev, isResult := result.(*EnumVal); isResult && ev.TypeName == "Result" {
			return ev
		}
		return makeOkResult(result)
	case <-time.After(ChatTimeout):
		return makeErrResult("chat_timeout", "agent chat exceeded "+ChatTimeout.String())
	}
}

// spawnAgent starts an agent's reasoning loop and returns an ActorRef the
// caller can `.chat(prompt)` on. Use spawnSupervisedAgent for crash callbacks.
func (interp *Interpreter) spawnAgent(decl *parser.AgentDecl) Value {
	return interp.spawnAgentWithCrash(decl, nil)
}

// spawnSupervisedAgent fires onCrash if the reasoning loop panics.
func (interp *Interpreter) spawnSupervisedAgent(decl *parser.AgentDecl, onCrash func(ref *ActorRef, reason string)) *ActorRef {
	v := interp.spawnAgentWithCrash(decl, onCrash)
	if ref, ok := v.(*ActorRef); ok {
		return ref
	}
	return nil
}

// spawnAgentWithCrash backs spawnAgent and spawnSupervisedAgent.
func (interp *Interpreter) spawnAgentWithCrash(decl *parser.AgentDecl, onCrash func(ref *ActorRef, reason string)) Value {
	if interp.llmClient == nil {
		// Return a dead ref whose .chat always replies Result.Err{llm_not_configured};
		// keeps `let g = spawn X(); g.chat(...)` uniform without an outer match.
		return spawnDeadAgent(decl.Name)
	}

	// Build a tool registry from the agent's declared tool references.
	reg := tool.NewRegistry()
	for _, toolName := range decl.Tools {
		schema := interp.GetToolSchema(toolName)
		if schema == nil {
			continue
		}
		name := toolName // capture for closure
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

	// Translate any output block into the JSON schema the loop validates.
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

	ag := agent.NewAgent(agent.AgentConfig{
		Model:              decl.Model,
		System:             decl.System,
		Tools:              toolSchemas,
		MaxTurns:           decl.MaxTurns,
		BudgetTokens:       decl.BudgetTokens,
		Temperature:        decl.Temperature,
		OutputSchema:       outputSchema,
		RetryInvalidOutput: decl.RetryInvalidOutput,
	}, interp.llmClient, reg)

	mailbox := make(chan ActorMessage, 256)
	done := make(chan struct{})

	ref := &ActorRef{
		Name:    decl.Name,
		Mailbox: mailbox,
		Done:    done,
	}

	// runAgent uses outputs to re-tag the JSON reply as <AgentName>.Output.
	go runAgent(ag, ref, mailbox, done, onCrash, decl.Name, decl.Outputs)
	return ref
}

// yoruTypeToJSONType maps a Yoru type to a JSON Schema type, defaulting to "string".
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

// spawnDeadAgent returns an ActorRef that replies Result.Err{llm_not_configured}
// to every chat. Fallback when SetLLMClient hasn't been called.
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

// runAgent drives an agent goroutine. Chat is encoded as ActorMessage with
// Method="chat" and Args["prompt"]. Panics fire onCrash when supervised.
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
			// Surface agent_output_invalid distinctly from generic agent_error.
			if strings.Contains(err.Error(), "agent_output_invalid") {
				msg.ReplyCh <- makeErrResult("agent_output_invalid", err.Error())
			} else {
				msg.ReplyCh <- makeErrResult("agent_error", err.Error())
			}
			continue
		}
		// With an output block, re-tag the validated JSON as <AgentName>.Output.
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

// jsonToOutputObject parses validated agent JSON into a typed ObjectVal so
// handlers can `r.field` directly. Extra (undeclared) fields pass through.
func jsonToOutputObject(jsonText, agentName string, outputs []parser.Field) (*ObjectVal, error) {
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

// jsonValueToYoruValue maps a parsed JSON value into the closest Yoru Value.
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
