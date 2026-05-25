package interpreter

import (
	"sync"
	"time"

	"github.com/adriangitvitz/yoru/parser"
)

// RestartStrategy determines how a supervisor responds to a child crash.
type RestartStrategy int

const (
	OneForOne  RestartStrategy = iota // only crashed child restarted
	OneForAll                         // all children restarted
	RestForOne                        // crashed child + all after it
)

// ChildSpec describes a supervised child. Exactly one of ActorDecl or
// AgentDecl must be set; both restart pathways are identical.
type ChildSpec struct {
	Name      string
	ActorDecl *parser.ActorDecl
	AgentDecl *parser.AgentDecl
	Args      map[string]Value
}

// childState tracks a running child and its crash history.
type childState struct {
	spec       ChildSpec
	ref        *ActorRef
	crashTimes []time.Time
}

// SupervisorConfig controls restart behavior.
type SupervisorConfig struct {
	Strategy      RestartStrategy
	MaxRestarts   int // max crashes per child within window before supervisor stops
	WindowSeconds int // rolling window for crash budget
}

// Supervisor monitors child actors and applies restart strategies on crash.
type Supervisor struct {
	interp   *Interpreter
	config   SupervisorConfig
	children []*childState
	mu       sync.Mutex
	stopped  bool
}

// NewSupervisor creates a supervisor with the given config and child specs.
// Call Start() to spawn children.
func NewSupervisor(interp *Interpreter, config SupervisorConfig, specs []ChildSpec) *Supervisor {
	children := make([]*childState, len(specs))
	for i, spec := range specs {
		children[i] = &childState{spec: spec}
	}
	return &Supervisor{
		interp:   interp,
		config:   config,
		children: children,
	}
}

// Start spawns all children with crash callbacks wired to this supervisor.
func (s *Supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, child := range s.children {
		child.ref = s.spawnChild(child.spec)
	}
	return nil
}

// spawnChild dispatches to the right spawn path based on which decl is set.
func (s *Supervisor) spawnChild(spec ChildSpec) *ActorRef {
	if spec.AgentDecl != nil {
		return s.interp.spawnSupervisedAgent(spec.AgentDecl, s.handleCrash)
	}
	return s.interp.spawnSupervisedActor(spec.ActorDecl, spec.Args, s.handleCrash)
}

// handleCrash is called (in a new goroutine) when a supervised actor panics.
func (s *Supervisor) handleCrash(ref *ActorRef, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}

	idx := -1
	for i, child := range s.children {
		if child.ref == ref {
			idx = i
			break
		}
	}
	if idx == -1 {
		return // ref no longer tracked (removed or already restarted by another handler)
	}

	now := time.Now()
	child := s.children[idx]
	child.crashTimes = append(child.crashTimes, now)

	windowStart := now.Add(-time.Duration(s.config.WindowSeconds) * time.Second)
	var inWindow []time.Time
	for _, t := range child.crashTimes {
		if !t.Before(windowStart) {
			inWindow = append(inWindow, t)
		}
	}
	child.crashTimes = inWindow

	if len(child.crashTimes) > s.config.MaxRestarts {
		s.stopAll()
		return
	}

	switch s.config.Strategy {
	case OneForOne:
		s.restartChild(idx)
	case OneForAll:
		for i := range s.children {
			s.restartChild(i)
		}
	case RestForOne:
		for i := idx; i < len(s.children); i++ {
			s.restartChild(i)
		}
	}
}

// restartChild stops the old actor/agent and spawns a fresh one. Caller must hold s.mu.
func (s *Supervisor) restartChild(idx int) {
	child := s.children[idx]
	oldRef := child.ref

	select {
	case <-oldRef.Done:
		// Already stopped (crashed)
	default:
		close(oldRef.Mailbox)
		<-oldRef.Done
	}

	child.ref = s.spawnChild(child.spec)
}

// Stop gracefully shuts down all children.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopAll()
}

// stopAll closes all children's mailboxes. Caller must hold s.mu.
func (s *Supervisor) stopAll() {
	s.stopped = true
	for _, child := range s.children {
		select {
		case <-child.ref.Done:
			// Already stopped
		default:
			close(child.ref.Mailbox)
			<-child.ref.Done
		}
	}
}

// Children returns the current ActorRefs for all children.
func (s *Supervisor) Children() []*ActorRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := make([]*ActorRef, len(s.children))
	for i, child := range s.children {
		refs[i] = child.ref
	}
	return refs
}

// AddChild dynamically registers and spawns a new child.
func (s *Supervisor) AddChild(spec ChildSpec) *ActorRef {
	s.mu.Lock()
	defer s.mu.Unlock()

	ref := s.spawnChild(spec)
	s.children = append(s.children, &childState{spec: spec, ref: ref})
	return ref
}

// RemoveChild stops and unregisters a child by ref.
func (s *Supervisor) RemoveChild(ref *ActorRef) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, child := range s.children {
		if child.ref == ref {
			select {
			case <-ref.Done:
			default:
				close(ref.Mailbox)
				<-ref.Done
			}
			s.children = append(s.children[:i], s.children[i+1:]...)
			return
		}
	}
}

// Stopped returns whether the supervisor has been stopped (e.g. crash budget exceeded).
func (s *Supervisor) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}
