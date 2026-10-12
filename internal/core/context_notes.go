// SPDX-License-Identifier: Apache-2.0
package core

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

const ContextBudget = 240000

type ContextSummary struct {
	Content string `json:"content"`
	Through int64  `json:"through_seq"`
	Applied bool   `json:"applied"`
}

func (v ContextSummary) Digest() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", v.Through, v.Content)))
	return hex.EncodeToString(sum[:])
}

type ContextPin struct {
	ID      int64  `json:"id"`
	Content string `json:"content"`
}

type ContextUsage struct {
	Bytes          int  `json:"bytes"`
	Budget         int  `json:"budget"`
	Percent        int  `json:"percent"`
	SummaryApplied bool `json:"summary_applied"`
	Pins           int  `json:"pins"`
}

func (s *Store) ordinaryContext(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM workflows WHERE id=?", id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("lifecycle context stays pinned; context summaries and pins are for ordinary sessions")
	}
	return nil
}

func (s *Store) lockContextChange(id string) (func(), error) {
	if err := s.ordinaryContext(id); err != nil {
		return nil, err
	}
	session, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	owner := "context:" + id + ":" + stamp()
	if err := s.Acquire(session.Repo, owner); err != nil {
		return nil, err
	}
	return func() { _ = s.Release(session.Repo, owner) }, nil
}

func (s *Store) ContextSummary(id string) (ContextSummary, error) {
	var v ContextSummary
	err := s.db.QueryRow("SELECT content,through_seq,applied FROM context_summaries WHERE session=?", id).Scan(&v.Content, &v.Through, &v.Applied)
	if err == sql.ErrNoRows {
		return v, nil
	}
	return v, err
}

func (s *Store) ContextDraft(id string) (ContextSummary, error) {
	var v ContextSummary
	err := s.db.QueryRow("SELECT content,through_seq,applied FROM context_summary_drafts WHERE session=?", id).Scan(&v.Content, &v.Through, &v.Applied)
	if err == sql.ErrNoRows {
		return v, nil
	}
	return v, err
}

// DraftContext makes an extractive starting point, not a claim to preserve every
// decision. It remains inactive until the user edits/reviews and explicitly applies it.
func (s *Store) DraftContext(id, reviewedText string) (ContextSummary, error) {
	unlock, err := s.lockContextChange(id)
	if err != nil {
		return ContextSummary{}, err
	}
	defer unlock()
	events, err := s.ContextEvents(id, 0)
	if err != nil {
		return ContextSummary{}, err
	}
	var through int64
	visible := []Event{}
	for _, e := range events {
		through = max(through, e.Seq)
		if e.Kind == "user" || e.Kind == "assistant" || e.Kind == "error" || e.Kind == "delegation" {
			visible = append(visible, e)
		}
	}
	if reviewedText != "" {
		existing, err := s.ContextDraft(id)
		if err != nil {
			return ContextSummary{}, err
		}
		if existing.Content != "" {
			through = existing.Through
		}
	}
	if reviewedText == "" {
		var b strings.Builder
		b.WriteString("EXTRACTIVE DRAFT — review and edit before applying. Excerpts can omit important details.\n\nGoal / acceptance criteria:\n")
		for _, e := range visible {
			if e.Kind == "user" {
				b.WriteString(contextExcerpt(e.Content, 1600) + "\n")
				break
			}
		}
		b.WriteString("\nDecisions, changes, verification and remaining work:\nReview the saved history and fill in anything absent from the excerpts below.\n")
		for _, e := range visible[max(0, len(visible)-8):] {
			fmt.Fprintf(&b, "\n[%s / %s]\n%s\n", e.Kind, e.Agent, contextExcerpt(e.Content, 1600))
		}
		reviewedText = b.String()
	}
	if strings.TrimSpace(reviewedText) == "" || len(reviewedText) > 64000 {
		return ContextSummary{}, fmt.Errorf("summary must contain between 1 and 64000 bytes")
	}
	v := ContextSummary{Content: reviewedText, Through: through}
	_, err = s.db.Exec("INSERT INTO context_summary_drafts VALUES(?,?,?,0) ON CONFLICT(session) DO UPDATE SET content=excluded.content,through_seq=excluded.through_seq,applied=0", id, v.Content, v.Through)
	return v, err
}

func contextExcerpt(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[excerpt truncated; full text remains in the journal]"
}

func (s *Store) ApplyContextSummary(id, digest string) error {
	unlock, err := s.lockContextChange(id)
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var v ContextSummary
	if err = tx.QueryRow("SELECT content,through_seq,applied FROM context_summary_drafts WHERE session=?", id).Scan(&v.Content, &v.Through, &v.Applied); err != nil {
		return fmt.Errorf("draft and review a summary first: %w", err)
	}
	if digest != "" && digest != v.Digest() {
		return fmt.Errorf("summary changed since review; review the current draft")
	}
	if _, err = tx.Exec("INSERT INTO context_summaries VALUES(?,?,?,1) ON CONFLICT(session) DO UPDATE SET content=excluded.content,through_seq=excluded.through_seq,applied=1", id, v.Content, v.Through); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM context_cursors WHERE session=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClearContextSummary(id string) error {
	unlock, err := s.lockContextChange(id)
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM context_summaries WHERE session=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM context_summary_drafts WHERE session=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM context_cursors WHERE session=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ContextPins(id string) ([]ContextPin, error) {
	rows, err := s.db.Query("SELECT id,content FROM context_pins WHERE session=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextPin{}
	for rows.Next() {
		var v ContextPin
		if err := rows.Scan(&v.ID, &v.Content); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) PinContext(id, content string) (int64, error) {
	unlock, err := s.lockContextChange(id)
	if err != nil {
		return 0, err
	}
	defer unlock()
	if strings.TrimSpace(content) == "" || len(content) > 16000 {
		return 0, fmt.Errorf("pin must contain between 1 and 16000 bytes")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.Exec("INSERT INTO context_pins(session,content) VALUES(?,?)", id, content)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec("DELETE FROM context_cursors WHERE session=?", id); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) UnpinContext(id string, pin int64) error {
	unlock, err := s.lockContextChange(id)
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("DELETE FROM context_pins WHERE session=? AND id=?", id, pin)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pin not found in this session")
	}
	if _, err = tx.Exec("DELETE FROM context_cursors WHERE session=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SearchContext(id, query string, limit int) ([]Event, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("search needs a query and a limit between 1 and 100")
	}
	rows, err := s.db.Query("SELECT * FROM events WHERE session=? AND kind IN ('user','assistant','delegation','error') AND instr(lower(content),lower(?))>0 ORDER BY seq DESC LIMIT ?", id, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Seq, &e.Session, &e.Kind, &e.Agent, &e.Content, &e.Created); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Runner) ContextUsage(s Session) (ContextUsage, error) {
	skills, err := r.Skills(s)
	if err != nil {
		return ContextUsage{}, err
	}
	names, err := r.Store.SkillSelection(s.ID)
	if err != nil {
		return ContextUsage{}, err
	}
	skills, err = FilterSkills(skills, names)
	if err != nil {
		return ContextUsage{}, err
	}
	text, err := r.buildContext(s, skills, 0, false)
	if err != nil {
		return ContextUsage{}, err
	}
	summary, err := r.Store.ContextSummary(s.ID)
	if err != nil {
		return ContextUsage{}, err
	}
	pins, err := r.Store.ContextPins(s.ID)
	if err != nil {
		return ContextUsage{}, err
	}
	return ContextUsage{Bytes: len(text), Budget: ContextBudget, Percent: len(text) * 100 / ContextBudget, SummaryApplied: summary.Applied, Pins: len(pins)}, nil
}
