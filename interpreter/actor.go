package interpreter

import (
	"fmt"
	"time"

	"github.com/adriangitvitz/yoru/parser"
)

// ActorMessage is the envelope sent to an actor's mailbox.
type ActorMessage struct {
	Method  string
	Args    map[string]Value
	ReplyCh chan Value // nil for fire-and-forget, non-nil for ask()
}

// Actor is the runtime representation of a spawned actor goroutine.
type Actor struct {
	name    string
	decl    *parser.ActorDecl
	mailbox chan ActorMessage
	done    chan struct{}
	state   *Environment
	interp  *Interpreter
	ref     *ActorRef                          // back-pointer for crash identification
	onCrash func(ref *ActorRef, reason string) // nil for unsupervised actors
}

// spawnActor creates an unsupervised actor and starts its goroutine.
func (interp *Interpreter) spawnActor(decl *parser.ActorDecl, args map[string]Value) *ActorRef {
	return interp.spawnSupervisedActor(decl, args, nil)
}

// spawnSupervisedActor creates an actor with optional crash callback and starts its goroutine.
func (interp *Interpreter) spawnSupervisedActor(
	decl *parser.ActorDecl,
	args map[string]Value,
	onCrash func(ref *ActorRef, reason string),
) *ActorRef {
	state := NewEnvironment()

	for _, sf := range decl.States {
		if sf.Default != nil {
			val := interp.evalExpression(sf.Default)
			state.Set(sf.Name, val)
		} else {
			state.Set(sf.Name, &NilVal{})
		}
	}

	// Override with constructor args
	for name, val := range args {
		state.Set(name, val)
	}

	mailbox := make(chan ActorMessage, 256)
	done := make(chan struct{})

	ref := &ActorRef{
		Name:    decl.Name,
		Mailbox: mailbox,
		Done:    done,
	}

	a := &Actor{
		name:    decl.Name,
		decl:    decl,
		mailbox: mailbox,
		done:    done,
		state:   state,
		interp:  interp,
		ref:     ref,
		onCrash: onCrash,
	}

	go a.run()

	return ref
}

// run processes messages sequentially. Panics are recovered; supervised
// actors notify their supervisor via onCrash, unsupervised actors exit cleanly.
func (a *Actor) run() {
	defer func() {
		r := recover()
		close(a.done) // always signal completion before crash callback
		if r != nil {
			reason := fmt.Sprintf("%v", r)
			if a.onCrash != nil {
				go a.onCrash(a.ref, reason)
			}
		}
	}()
	for msg := range a.mailbox {
		result := a.handleMessage(msg)
		if msg.ReplyCh != nil {
			msg.ReplyCh <- result
		}
	}
}

// handleMessage finds the matching receive block and evaluates it.
func (a *Actor) handleMessage(msg ActorMessage) Value {
	for _, rb := range a.decl.Receives {
		if rb.MessageType == msg.Method {
			return a.evalReceive(&rb, msg.Args)
		}
	}
	return &NilVal{}
}

// evalReceive evaluates a receive block body with message args bound.
func (a *Actor) evalReceive(rb *parser.ReceiveBlock, args map[string]Value) Value {
	// Create a child interpreter that shares the actor's effect stack
	child := &Interpreter{
		env:             NewEnclosedEnvironment(a.state),
		effectStack:     a.interp.effectStack,
		runtimeEffects:  a.interp.runtimeEffects,
		objectDecls:     a.interp.objectDecls,
		enumDecls:       a.interp.enumDecls,
		actorDecls:      a.interp.actorDecls,
		pipelineDecls:   a.interp.pipelineDecls,
		toolDecls:       a.interp.toolDecls,
		agentDecls:      a.interp.agentDecls,
		mcpDecls:        a.interp.mcpDecls,
		serviceDecls:    a.interp.serviceDecls,
		llmClient:       a.interp.llmClient,
		capabilityStack: a.interp.capabilityStack,
	}
	// Register builtins so actors can use min(), len(), etc.
	child.registerBuiltins()

	// Bind self as a proxy to actor state
	child.env.Set("self", &actorSelf{state: a.state})

	for _, p := range rb.Params {
		if val, ok := args[p.Name]; ok {
			child.env.Set(p.Name, val)
		}
	}

	result := child.evalBlock(rb.Body)
	if rs, ok := result.(*ReturnSignal); ok {
		return rs.Value
	}
	return result
}

// AskActor sends a synchronous message to an actor and waits for a reply.
// This is the Go-bridge equivalent of actor.ask(MessageType) in Yoru code.
func (interp *Interpreter) AskActor(ref Value, method string, args map[string]Value) (Value, error) {
	actorRef, ok := ref.(*ActorRef)
	if !ok {
		return nil, fmt.Errorf("AskActor: not an actor reference")
	}
	if args == nil {
		args = make(map[string]Value)
	}
	msg := ActorMessage{
		Method:  method,
		Args:    args,
		ReplyCh: make(chan Value, 1),
	}
	actorRef.Mailbox <- msg

	select {
	case result := <-msg.ReplyCh:
		return result, nil
	case <-time.After(AskTimeout):
		return nil, fmt.Errorf("AskActor: timeout waiting for reply")
	}
}

// AskTimeout is the maximum duration an `actor.ask(Message)` call will wait for
// a reply before returning a Result.Err{kind: "ask_timeout"}.
var AskTimeout = 5 * time.Second

// actorSelf proxies field access/update to the actor's state environment.
type actorSelf struct {
	state *Environment
}

func (s *actorSelf) Type() string        { return "Self" }
func (s *actorSelf) Inspect() string     { return "<self>" }
func (s *actorSelf) Equals(o Value) bool { return s == o }

// GetField reads from actor state.
func (s *actorSelf) GetField(name string) (Value, bool) {
	return s.state.Get(name)
}

// SetField updates actor state.
func (s *actorSelf) SetField(name string, val Value) {
	s.state.Update(name, val)
}
