package interpreter

// EffectHandler binds an effect name to a provider (an ObjectVal with method fields).
type EffectHandler struct {
	EffectName string
	Provider   Value
}

// EffectStack is a dynamic scope stack for effect handlers. Resolve is the
// hot path (every effect op inside pipelines/tools/services), so we cache
// the top-most-handler map and rebuild eagerly on Push/Pop.
type EffectStack struct {
	handlers []EffectHandler
	cache    map[string]Value
}

// NewEffectStack creates an empty effect stack.
func NewEffectStack() *EffectStack {
	return &EffectStack{cache: make(map[string]Value)}
}

// rebuildCache walks handlers bottom-up so the top-most provider per name wins.
// Uses clear() to reuse the existing bucket array, avoiding per-Push/Pop allocs.
func (s *EffectStack) rebuildCache() {
	if s.cache == nil {
		s.cache = make(map[string]Value, len(s.handlers))
	} else {
		clear(s.cache)
	}
	for _, h := range s.handlers {
		s.cache[h.EffectName] = h.Provider
	}
}

// Push adds a handler to the top of the stack.
func (s *EffectStack) Push(h EffectHandler) {
	s.handlers = append(s.handlers, h)
	s.rebuildCache()
}

// Pop removes the topmost handler.
func (s *EffectStack) Pop() {
	if len(s.handlers) > 0 {
		s.handlers = s.handlers[:len(s.handlers)-1]
		s.rebuildCache()
	}
}

// Resolve returns the top-most handler matching effectName. O(1) via cache.
func (s *EffectStack) Resolve(effectName string) (Value, bool) {
	v, ok := s.cache[effectName]
	return v, ok
}

// EffectProvider is the interface for stdlib effect namespaces.
// Each provider supplies a name and a map of methods (BuiltinVal entries).
type EffectProvider interface {
	EffectName() string
	Methods() map[string]Value
}

// InstallProvider registers an EffectProvider at the bottom of the effect stack.
// User-level handle() blocks still override stdlib providers.
func (interp *Interpreter) InstallProvider(p EffectProvider) {
	handler := EffectHandler{
		EffectName: p.EffectName(),
		Provider:   &ObjectVal{TypeName: p.EffectName(), Fields: p.Methods()},
	}
	interp.effectStack.Push(handler)
}
