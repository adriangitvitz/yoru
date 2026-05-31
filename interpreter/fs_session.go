package interpreter

import "sync"

// FSSession tracks reads observed within a scope so the FS provider can
// reject tracked writes against unread or stale paths.
type FSSession struct {
	mu      sync.Mutex
	records map[string]FSSessionRecord
}

type FSSessionRecord struct {
	Hash    string
	ModUnix int64
}

func newFSSession() *FSSession {
	return &FSSession{records: make(map[string]FSSessionRecord)}
}

func (s *FSSession) Record(path, hash string, modUnix int64) {
	s.mu.Lock()
	s.records[path] = FSSessionRecord{Hash: hash, ModUnix: modUnix}
	s.mu.Unlock()
}

func (s *FSSession) Get(path string) (FSSessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[path]
	return r, ok
}

// PushFSSession opens a fresh tracker scope. Nested sessions don't inherit parent records.
func (interp *Interpreter) PushFSSession() *FSSession {
	s := newFSSession()
	interp.fsSessionStack = append(interp.fsSessionStack, s)
	return s
}

func (interp *Interpreter) PopFSSession() {
	if len(interp.fsSessionStack) == 0 {
		return
	}
	interp.fsSessionStack = interp.fsSessionStack[:len(interp.fsSessionStack)-1]
}

// CurrentFSSession returns the topmost session or nil when none is open.
func (interp *Interpreter) CurrentFSSession() *FSSession {
	if len(interp.fsSessionStack) == 0 {
		return nil
	}
	return interp.fsSessionStack[len(interp.fsSessionStack)-1]
}
