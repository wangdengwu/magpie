package teamcore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/steady"
)

// The files teamd keeps, under their state folder.
const (
	// TeamsFile holds the teams, and the hash of each one's token.
	TeamsFile = "teams.json"
	// AuditFile holds one JSON line per change the portal made or
	// refused, oldest first, and is only ever appended to.
	AuditFile = "audit.jsonl"
	// AuditOld is the one generation kept when audit.jsonl is rotated: the
	// file before the last rotation. The one before that is dropped, so a
	// long-lived portal can't fill the volume with history nobody reads.
	AuditOld = "audit.jsonl.1"
	// LockFile is what the ops commands and a running sidecar take, so a
	// change one makes is seen whole by the other.
	LockFile = "teamd.lock"
)

// DefaultAuditMax is how big audit.jsonl may grow before it is rotated.
// A change is a line of about 200 bytes, so this is tens of thousands of
// changes — a portal's whole life at a team's pace — in 4 MiB.
const DefaultAuditMax = 4 << 20

// Store is teamd's own files. Two of them: teams.json, which ops writes
// and the sidecar reads, and audit.jsonl, which only grows.
//
// teams.json is replaced whole, through a temp file renamed over it, so a
// reader never sees half of one; the lock is for the read-modify-write a
// change makes, which two processes doing at once would lose one of.
type Store struct {
	dir string
	mu  sync.Mutex // this process's writers; LockFile is other processes'
	// max is the size audit.jsonl rotates at, 0 for DefaultAuditMax and
	// a negative for never.
	max int64
}

// NewStore opens the state folder, making it if it isn't there. max is the
// size audit.jsonl rotates at, 0 for DefaultAuditMax and a negative for
// never.
func NewStore(dir string, max int64) (*Store, error) {
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("teamd's state folder %s can't be made: %w", dir, err)
	}
	return &Store{dir: dir, max: max}, nil
}

// Dir is the state folder.
func (s *Store) Dir() string { return s.dir }

// Teams are the teams as teams.json holds them. No file is no teams, the
// state a fresh install starts in. A file that can't be read or parsed is
// an error and never no teams: every page it feeds is then refused,
// instead of a team's link quietly opening nothing.
func (s *Store) Teams() ([]Team, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, TeamsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("teamd's teams can't be read: %w", err)
	}
	var teams []Team
	if err := json.Unmarshal(b, &teams); err != nil {
		return nil, fmt.Errorf("teamd's %s doesn't read: %w", TeamsFile, err)
	}
	return teams, nil
}

// Save replaces teams.json with teams, whole and at once, under the lock
// both processes share.
func (s *Store) Save(teams []Team) error {
	release := s.hold()
	defer release()
	return s.save(teams)
}

// save writes teams.json with the lock already held.
func (s *Store) save(teams []Team) error {
	if teams == nil {
		teams = []Team{}
	}
	b, err := json.MarshalIndent(teams, "", "  ")
	if err != nil {
		return err
	}
	return teamdWrite(filepath.Join(s.dir, TeamsFile), append(b, '\n'))
}

// Update changes the teams under the lock both processes share: fn reads
// what is there now and answers what to keep. Nothing is written when fn
// fails, so a refused change leaves teams.json as it was. Every command
// that changes a team goes through here, and so does every read that is
// about to be written back.
func (s *Store) Update(fn func([]Team) ([]Team, error)) error {
	release := s.hold()
	defer release()
	teams, err := s.Teams()
	if err != nil {
		return err
	}
	next, err := fn(teams)
	if err != nil {
		return err
	}
	// A file that couldn't be read is an error above, never an empty list
	// written back over every team.
	return s.save(next)
}

// hold takes the folder: this process's writers, then the lock another
// process takes on it, and answers what lets both go. The file lock is
// held only while the folder is opened and closed, never across a
// request.
func (s *Store) hold() func() {
	s.mu.Lock()
	unlock, _ := filelock.Lock(filepath.Join(s.dir, LockFile), filelock.Wait)
	return func() {
		unlock()
		s.mu.Unlock()
	}
}

// Entry is one line of audit.jsonl: a change the portal made, or one it
// refused.
//
// The fields are what ops needs to tell who did what. A token, a key and a
// callback's query are secrets and are never among them: Result says how
// an action went, not what it was sent.
type Entry struct {
	At     time.Time `json:"at"`
	Team   string    `json:"team"`
	Action string    `json:"action"`
	// Account is the group entry the action named, "<provider>/<stable
	// id>". The name it was shown by is Name, so a line still reads after
	// the account is gone.
	Account string `json:"account,omitempty"`
	Name    string `json:"name,omitempty"`
	// Who made the change: the team whose link asked, or OpsWho for a
	// command run by hand.
	Who    string `json:"who,omitempty"`
	Result string `json:"result"`
}

// OpsWho is who an audit line names for a change made by an ops command.
const OpsWho = "ops"

// The results an entry can carry. Allowed rather than refused is the one
// that matters most on this page: a refusal is what a lead needs to see
// when a key was used by something that shouldn't have had it.
const (
	ResultOK     = "ok"
	ResultDenied = "refused"
)

// Audit appends one line to audit.jsonl, rotating the file first when it
// has grown past max. One Write of one line, so a line is whole however
// many processes are appending.
//
// Rotation renames the file aside rather than rewriting it: a reader in
// another process holding the old name keeps reading the bytes it opened,
// and a crash between the rename and the next write loses nothing that was
// written. The generation before the last is dropped.
func (s *Store) Audit(e Entry) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, AuditFile)
	if max := s.auditMax(); max > 0 {
		if fi, err := os.Stat(path); err == nil && fi.Size()+int64(len(b))+1 > max {
			// a rotation that fails is not a lost line: the line is still
			// written, and the file is tried again next time
			_ = os.Rename(path, filepath.Join(s.dir, AuditOld))
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("the audit file can't be opened: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("the audit line can't be written: %w", err)
	}
	return f.Close()
}

// auditMax is how big the audit file may grow before it rotates, 0 for
// never.
func (s *Store) auditMax() int64 {
	switch {
	case s.max < 0:
		return 0
	case s.max == 0:
		return DefaultAuditMax
	}
	return s.max
}

// AuditMax is the size audit.jsonl rotates at, 0 for never.
func (s *Store) AuditMax() int64 { return s.auditMax() }

// AuditLines are the last n audit lines for one team, oldest first, and
// how many of that team's lines there are in all — so a page can say it is
// showing the last few of more.
//
// teamID "" reads every team's, which is what ops wants; a lead's page
// passes its own, and a line of another team's is never returned. A line
// that doesn't parse is skipped rather than failing the read: half a line
// from a writer that died mid-write is not a reason to show nothing.
//
// The rotated generation is read too, when there is one, so a page doesn't
// go blank the moment the file turns over.
func (s *Store) AuditLines(teamID string, n int) (entries []Entry, total int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// the older generation first: the two files together are oldest-first
	// only in that order
	var all []Entry
	for _, name := range []string{AuditOld, AuditFile} {
		b, err := os.ReadFile(filepath.Join(s.dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, 0, fmt.Errorf("the audit file %s can't be read: %w", name, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var e Entry
			if json.Unmarshal([]byte(line), &e) != nil {
				continue
			}
			if teamID != "" && !strings.EqualFold(e.Team, teamID) {
				continue
			}
			all = append(all, e)
		}
	}
	total = len(all)
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all, total, nil
}

// teamdWrite replaces path with b, 0600: teams.json holds the hashes of
// team tokens, and audit.jsonl what each team did, which are for teamd and
// ops alone.
func teamdWrite(path string, b []byte) error {
	return steady.WriteFile(path, b, 0o600)
}
