package bot

import (
	"sync"

	"osint.bot/internal/model"
)

type UserStateStore struct {
	mu    sync.RWMutex
	state map[int64]string
	cache map[int64]*model.SearchResult
}

func NewUserStateStore() *UserStateStore {
	return &UserStateStore{
		state: make(map[int64]string),
		cache: make(map[int64]*model.SearchResult),
	}
}

func (s *UserStateStore) SetState(userID int64, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[userID] = state
}

func (s *UserStateStore) PopState(userID int64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.state[userID]
	if ok {
		delete(s.state, userID)
	}
	return state, ok
}

func (s *UserStateStore) SetCache(userID int64, result *model.SearchResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[userID] = result
}

func (s *UserStateStore) ClearCache(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, userID)
}
