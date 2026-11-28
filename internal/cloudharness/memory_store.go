package cloudharness

import "sync"

type MemoryStore struct {
	stepMu  sync.Mutex
	mu      sync.RWMutex
	threads map[ThreadID]memoryThread
}

type memoryThread struct {
	pending    Work
	messages   []MessageWithID
	exists     bool
	parent     *ThreadParentInfo
	spawnOrder int64
	closed     bool
	fresh      bool
	compaction bool
	summaries  []CompactionSummary
	values     memoryState
}

var _ StateStore = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{threads: make(map[ThreadID]memoryThread)}
}

func (s *MemoryStore) setPending(threadID ThreadID, work Work) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	thread.pending = work
	s.threads[threadID] = thread
}
