// SPDX-License-Identifier: Apache-2.0
package core

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Session struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Agent   string `json:"agent"`
	Parent  string `json:"parent,omitempty"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}
type Event struct {
	Seq     int64  `json:"seq"`
	Session string `json:"session"`
	Kind    string `json:"kind"`
	Agent   string `json:"agent"`
	Content string `json:"content"`
	Created string `json:"created"`
}
type Store struct{ db *sql.DB }

func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func OpenStore(home string) (*Store, error) {
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(home, "sessions.db")
	// Create the journal with private permissions before SQLite opens it.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, repo TEXT NOT NULL, agent TEXT NOT NULL, parent TEXT NOT NULL DEFAULT '', created TEXT NOT NULL, updated TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT, session TEXT NOT NULL, kind TEXT NOT NULL, agent TEXT NOT NULL, content TEXT NOT NULL, created TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS events_session ON events(session,seq);
 CREATE TABLE IF NOT EXISTS native(session TEXT NOT NULL,agent TEXT NOT NULL,id TEXT NOT NULL,PRIMARY KEY(session,agent));
 CREATE TABLE IF NOT EXISTS skills(session TEXT PRIMARY KEY,manifest TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS leases(repo TEXT PRIMARY KEY, owner TEXT NOT NULL, expires INTEGER NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Create(repo, agent, parent string) (Session, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return Session{}, err
	}
	v := Session{ID: hex.EncodeToString(b), Repo: repo, Agent: agent, Parent: parent, Created: stamp()}
	v.Updated = v.Created
	_, err := s.db.Exec("INSERT INTO sessions VALUES(?,?,?,?,?,?)", v.ID, v.Repo, v.Agent, v.Parent, v.Created, v.Updated)
	return v, err
}
func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var v Session
	err := row.Scan(&v.ID, &v.Repo, &v.Agent, &v.Parent, &v.Created, &v.Updated)
	return v, err
}
func (s *Store) Get(id string) (Session, error) {
	v, e := scanSession(s.db.QueryRow("SELECT * FROM sessions WHERE id=?", id))
	if e == sql.ErrNoRows {
		return v, fmt.Errorf("session %q not found", id)
	}
	return v, e
}
func (s *Store) Latest(repo string) (Session, error) {
	v, e := scanSession(s.db.QueryRow("SELECT * FROM sessions WHERE repo=? AND parent='' ORDER BY updated DESC LIMIT 1", repo))
	if e == sql.ErrNoRows {
		return v, fmt.Errorf("no session in %s", repo)
	}
	return v, e
}
func (s *Store) List() ([]Session, error) {
	rows, e := s.db.Query("SELECT * FROM sessions ORDER BY updated DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		v, e := scanSession(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Switch(id, agent string) error {
	_, e := s.db.Exec("UPDATE sessions SET agent=?,updated=? WHERE id=?", agent, stamp(), id)
	return e
}
func (s *Store) Append(id, kind, agent, content string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec("INSERT INTO events(session,kind,agent,content,created) VALUES(?,?,?,?,?)", id, kind, agent, content, stamp())
	if e != nil {
		return e
	}
	_, e = tx.Exec("UPDATE sessions SET updated=? WHERE id=?", stamp(), id)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Events(id string) ([]Event, error) {
	rows, e := s.db.Query("SELECT * FROM events WHERE session=? ORDER BY seq", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.Seq, &v.Session, &v.Kind, &v.Agent, &v.Content, &v.Created); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Native(id, agent string) (string, error) {
	var v string
	e := s.db.QueryRow("SELECT id FROM native WHERE session=? AND agent=?", id, agent).Scan(&v)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return v, e
}
func (s *Store) SetNative(id, agent, native string) error {
	_, e := s.db.Exec("INSERT OR REPLACE INTO native VALUES(?,?,?)", id, agent, native)
	return e
}
func (s *Store) SkillManifest(id string) ([]Skill, bool, error) {
	var raw string
	e := s.db.QueryRow("SELECT manifest FROM skills WHERE session=?", id).Scan(&raw)
	if e == sql.ErrNoRows {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	var out []Skill
	e = json.Unmarshal([]byte(raw), &out)
	return out, true, e
}
func (s *Store) SetSkills(id string, skills []Skill) error {
	b, e := json.Marshal(skills)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT OR REPLACE INTO skills VALUES(?,?)", id, string(b))
	return e
}

// A repository lease serializes parent coding runs even across Crew processes.
func (s *Store) Acquire(repo, owner string) error {
	now := time.Now().Unix()
	result, e := s.db.Exec(`INSERT INTO leases VALUES(?,?,?) ON CONFLICT(repo) DO UPDATE SET owner=excluded.owner,expires=excluded.expires WHERE leases.expires<?`, repo, owner, now+30, now)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return fmt.Errorf("another Crew task is active in this repository")
	}
	return nil
}
func (s *Store) Renew(repo, owner string) error {
	r, e := s.db.Exec("UPDATE leases SET expires=? WHERE repo=? AND owner=?", time.Now().Unix()+30, repo, owner)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n != 1 {
		return fmt.Errorf("repository lease lost")
	}
	return e
}
func (s *Store) Release(repo, owner string) error {
	_, e := s.db.Exec("DELETE FROM leases WHERE repo=? AND owner=?", repo, owner)
	return e
}
