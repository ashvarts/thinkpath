package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// A Step is one prompt-and-reply in a session, as judged by the agent against
// the session goal. Direction says which way it moved, Distance says how far
// (1 = small step, 5 = big leap), and Ghost optionally names the trap the
// recent steps seem to be drifting towards.
type Step struct {
	At        time.Time `json:"at"`
	Direction string    `json:"direction"`
	Distance  int       `json:"distance"`
	Summary   string    `json:"summary"`
	Ghost     string    `json:"ghost,omitempty"`
}

// A Session is one goal pursued in one project. Each project has at most one
// open session at a time, but different projects can have sessions open side
// by side.
type Session struct {
	ID        int        `json:"id"`
	Project   string     `json:"project"`
	Goal      string     `json:"goal"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Reached   bool       `json:"reached"`
	Steps     []Step     `json:"steps"`
}

type Trap struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// The known procrastination patterns a ghost line can point at. The web page
// has a matching position for each name, so keep the two in sync.
var traps = []Trap{
	{"yak-shaving", "fixing a chain of prerequisites instead of the task"},
	{"bikeshedding", "arguing over small details instead of the main problem"},
	{"rabbit-hole", "going deeper and deeper into a curiosity"},
	{"tool-tinkering", "adjusting your setup or tools instead of using them"},
	{"premature-optimization", "perfecting something before it works"},
	{"research-spiral", "reading more without deciding anything"},
	{"scope-creep", "the goal quietly growing bigger"},
}

var directions = []string{"toward", "stalled", "sideways"}

var (
	ErrNoSession    = errors.New("no session yet: set a goal first")
	ErrSessionEnded = errors.New("the current session has ended: set a new goal first")
)

// A ValidationError means the caller sent something malformed, as opposed to
// the store being in the wrong state for the request.
type ValidationError struct{ msg string }

func (e ValidationError) Error() string { return e.msg }

func validateStep(st *Step) error {
	if !slices.Contains(directions, st.Direction) {
		return ValidationError{fmt.Sprintf("direction must be one of %s", strings.Join(directions, ", "))}
	}

	// A stalled step doesn't go anywhere, so we ignore whatever distance was
	// sent rather than making the caller remember to pass zero.
	if st.Direction == "stalled" {
		st.Distance = 0
	} else if st.Distance < 1 || st.Distance > 5 {
		return ValidationError{"distance must be between 1 and 5"}
	}

	if st.Ghost != "" && !slices.ContainsFunc(traps, func(t Trap) bool { return t.Name == st.Ghost }) {
		names := make([]string, len(traps))
		for i, t := range traps {
			names[i] = t.Name
		}
		return ValidationError{fmt.Sprintf("ghost must be one of %s", strings.Join(names, ", "))}
	}

	if strings.TrimSpace(st.Summary) == "" {
		return ValidationError{"summary must not be empty"}
	}
	return nil
}

// Store keeps every session in memory and writes the whole lot to a single
// JSON file after each change. The data is tiny, so that's plenty.
type Store struct {
	mu       sync.Mutex
	path     string
	sessions []Session
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &s.sessions); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return s, nil
}

// save must be called with s.mu held. It writes to a temporary file and
// renames it so a crash halfway through can't leave a corrupt data file.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Sessions are handed out as copies so callers can encode them without
// holding the lock.
func cloneSession(sess Session) Session {
	sess.Steps = slices.Clone(sess.Steps)
	return sess
}

func (s *Store) Sessions() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Session, len(s.sessions))
	for i, sess := range s.sessions {
		out[i] = cloneSession(sess)
	}
	return out
}

// Current returns the latest session for the project, open or not.
func (s *Store) Current(project string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur := s.latest(project)
	if cur == nil {
		return Session{}, ErrNoSession
	}
	return cloneSession(*cur), nil
}

// StartSession begins a new session in the project with the given goal. If
// the project's previous session is still open it gets closed as not
// reached, because a project only ever has one goal in play at a time.
func (s *Store) StartSession(project, goal string) (Session, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Session{}, ValidationError{"goal must not be empty"}
	}
	if project == "" {
		return Session{}, ValidationError{"project must not be empty"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if prev := s.latest(project); prev != nil && prev.EndedAt == nil {
		prev.EndedAt = &now
	}

	sess := Session{
		ID:        len(s.sessions) + 1,
		Project:   project,
		Goal:      goal,
		StartedAt: now,
		Steps:     []Step{},
	}
	s.sessions = append(s.sessions, sess)

	if err := s.save(); err != nil {
		return Session{}, err
	}
	return cloneSession(sess), nil
}

func (s *Store) AddStep(project string, st Step) (Session, error) {
	if err := validateStep(&st); err != nil {
		return Session{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, err := s.open(project)
	if err != nil {
		return Session{}, err
	}

	st.At = time.Now().UTC()
	st.Summary = strings.TrimSpace(st.Summary)
	cur.Steps = append(cur.Steps, st)

	if err := s.save(); err != nil {
		return Session{}, err
	}
	return cloneSession(*cur), nil
}

// Finish closes the project's current session. reached says whether the goal
// was met or the session was abandoned.
func (s *Store) Finish(project string, reached bool) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, err := s.open(project)
	if err != nil {
		return Session{}, err
	}

	now := time.Now().UTC()
	cur.EndedAt = &now
	cur.Reached = reached

	if err := s.save(); err != nil {
		return Session{}, err
	}
	return cloneSession(*cur), nil
}

// latest returns a pointer to the project's newest session, or nil if it
// has none. It must be called with s.mu held.
func (s *Store) latest(project string) *Session {
	for i := len(s.sessions) - 1; i >= 0; i-- {
		if s.sessions[i].Project == project {
			return &s.sessions[i]
		}
	}
	return nil
}

// open returns a pointer to the project's newest session, as long as it
// hasn't ended. It must be called with s.mu held.
func (s *Store) open(project string) (*Session, error) {
	cur := s.latest(project)
	if cur == nil {
		return nil, ErrNoSession
	}
	if cur.EndedAt != nil {
		return nil, ErrSessionEnded
	}
	return cur, nil
}
