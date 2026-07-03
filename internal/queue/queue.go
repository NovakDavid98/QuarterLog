package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Status of a pending interval.
const (
	StatusPending = "pending" // captured, awaiting user action
	StatusLocked  = "locked"  // machine was locked/asleep; no useful screenshot, type-it-yourself
)

// Interval is one quarter-hour of work awaiting a log entry.
type Interval struct {
	ID       string    `json:"id"`
	Date     string    `json:"date"`     // YYYY-MM-DD (local)
	From     string    `json:"from"`     // HH:MM boundary start
	To       string    `json:"to"`       // HH:MM boundary end
	Hours    float64   `json:"hours"`    // decimal, e.g. 0.25
	Status   string    `json:"status"`   // StatusPending | StatusLocked
	ImagePath string   `json:"imagePath"` // absolute path to full PNG (empty when locked)
	Created  time.Time `json:"created"`
}

// Store is a mutex-protected persistent queue of pending intervals.
type Store struct {
	mu    sync.Mutex
	dir   string
	index string
	items map[string]*Interval
}

// Open loads (or creates) the queue store under %LOCALAPPDATA%\Quarterlog\queue.
func Open() (*Store, error) {
	base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, "Quarterlog", "queue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:   dir,
		index: filepath.Join(dir, "index.json"),
		items: map[string]*Interval{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the directory where interval images should be written.
func (s *Store) Dir() string { return s.dir }

func (s *Store) load() error {
	data, err := os.ReadFile(s.index)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []*Interval
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	for _, it := range list {
		s.items[it.ID] = it
	}
	return nil
}

// persist writes the index; caller must hold the lock.
func (s *Store) persist() error {
	list := make([]*Interval, 0, len(s.items))
	for _, it := range s.items {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created.Before(list[j].Created) })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.index + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.index)
}

// Add stores a new interval and persists the index.
func (s *Store) Add(it *Interval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[it.ID] = it
	return s.persist()
}

// Get returns a copy of the interval with the given id.
func (s *Store) Get(id string) (Interval, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return Interval{}, false
	}
	return *it, true
}

// List returns all pending intervals sorted oldest-first.
func (s *Store) List() []Interval {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]Interval, 0, len(s.items))
	for _, it := range s.items {
		list = append(list, *it)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created.Before(list[j].Created) })
	return list
}

// Count returns the number of pending intervals.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Remove deletes an interval and its image file.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if it, ok := s.items[id]; ok {
		if it.ImagePath != "" {
			_ = os.Remove(it.ImagePath)
		}
		delete(s.items, id)
	}
	return s.persist()
}
