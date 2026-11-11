package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"handful/internal/db"
)

// States of a question: a bot message with needsReply. A question stays open
// until the user answers or dismisses it, or its bot withdraws it; later
// posts leave it open.
const (
	QuestionOpen      = "open"
	QuestionAnswered  = "answered"
	QuestionDismissed = "dismissed"
	QuestionWithdrawn = "withdrawn"
)

const (
	// MaxOpenQuestions is how many open questions a bot can have in one chat.
	MaxOpenQuestions = 20
	// maxQuestionLen is the length of a question's one-line summary.
	maxQuestionLen = 140
	// reopenWindowMs is how long (ms) after a dismissal the user can undo it.
	reopenWindowMs = 10_000
	// closedWindowMs is how far back (ms) the Waiting view counts closed
	// questions: 7 days.
	closedWindowMs = 604_800_000
)

// OpenQuestion is a question that waits for the user.
type OpenQuestion struct {
	EntryID   string `json:"entryId"`
	Seq       int64  `json:"seq"`
	ChatID    string `json:"chatId"` // the bot or room the question is in
	AskerID   string `json:"askerId"`
	AskerName string `json:"askerName"`
	// Question is the bot's one-line summary, or the first line of its text.
	Question  string `json:"question"`
	CreatedAt int64  `json:"createdAt"`
	// State and ClosedAt are set only on the closed questions of Waiting.
	State    string `json:"state,omitempty"`
	ClosedAt int64  `json:"closedAt,omitempty"`
}

// Waiting is everything that waits for the user, for the Waiting view.
type Waiting struct {
	Questions   []OpenQuestion `json:"questions"`
	Approvals   int64          `json:"approvals"`   // bots and rooms with a pending approval
	PausedGoals int64          `json:"pausedGoals"` // teams whose goal is paused
	// ClosedRecently counts the questions answered, dismissed or withdrawn
	// in the last 7 days.
	ClosedRecently int64 `json:"closedRecently"`
	// Closed lists those questions, newest first.
	Closed []OpenQuestion `json:"closed"`
}

// questionBody is what a question keeps in its entry's body.
type questionBody struct {
	Text          string `json:"text"`
	Question      string `json:"question"`
	QuestionState string `json:"questionState"`
	SpeakerID     string `json:"speakerId"`
	SpeakerName   string `json:"speakerName"`
	ClosedAt      int64  `json:"closedAt"`
	GoalID        string `json:"goalId"`
}

func parseQuestion(row db.Entry) (questionBody, error) {
	var b questionBody
	if err := json.Unmarshal([]byte(row.Body), &b); err != nil {
		return questionBody{}, fmt.Errorf("decode entry %s: %w", row.ID, err)
	}
	return b, nil
}

// asker is who asked a question: the speaker of a room post, or the bot of
// its direct chat.
func (b questionBody) asker(row db.Entry) string {
	if b.SpeakerID != "" {
		return b.SpeakerID
	}
	return row.BotID
}

// summary is the line that lists a question: its own line, or the first
// line of its text.
func (b questionBody) summary() string {
	if b.Question != "" {
		return b.Question
	}
	line, _, _ := strings.Cut(strings.TrimSpace(b.Text), "\n")
	return cut(strings.TrimSpace(line), maxQuestionLen)
}

// questionLine checks the one-line summary of a post: it goes only with
// needsReply. With needsReply, the asker must have room for one more open
// question in the chat.
func (s *Service) questionLine(ctx context.Context, chatID, askerID string, needsReply bool, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question != "" && !needsReply {
		return "", invalid("a question line goes only with needs_reply")
	}
	if utf8.RuneCountInString(question) > maxQuestionLen {
		return "", invalid("the question line is longer than %d characters", maxQuestionLen)
	}
	if !needsReply {
		return "", nil
	}
	open, err := s.OpenQuestionsOf(ctx, chatID, askerID)
	if err != nil {
		return "", err
	}
	if len(open) >= MaxOpenQuestions {
		return "", invalid("You have %d open questions here; close some with closes_questions first.", MaxOpenQuestions)
	}
	return question, nil
}

// addQuestion marks the body of a post with needsReply as an open question.
func addQuestion(body map[string]any, needsReply bool, question string) {
	if !needsReply {
		return
	}
	body["questionState"] = QuestionOpen
	if question != "" {
		body["question"] = question
	}
}

// questionsOf lists the open questions of a chat with q; askerID "" lists
// every asker's.
func (s *Service) questionsOf(ctx context.Context, q *db.Queries, chatID, askerID string) ([]OpenQuestion, error) {
	rows, err := q.ListOpenQuestionEntries(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("list the open questions of %s: %w", chatID, err)
	}
	return s.toQuestions(ctx, q, rows, askerID)
}

// toQuestions makes the questions of entries, the ones of askerID only when
// it is not "".
func (s *Service) toQuestions(ctx context.Context, q *db.Queries, rows []db.Entry, askerID string) ([]OpenQuestion, error) {
	out := []OpenQuestion{}
	names := map[string]string{}
	for _, row := range rows {
		b, err := parseQuestion(row)
		if err != nil {
			return nil, err
		}
		asker := b.asker(row)
		if askerID != "" && asker != askerID {
			continue
		}
		name, err := s.askerName(ctx, q, names, asker, b.SpeakerName)
		if err != nil {
			return nil, err
		}
		out = append(out, OpenQuestion{EntryID: row.ID, Seq: row.Seq, ChatID: row.BotID, AskerID: asker, AskerName: name, Question: b.summary(), CreatedAt: row.CreatedAt})
	}
	return out, nil
}

// askerName names the asker of a question: the speaker name of a room post,
// else the bot's name, looked up once per bot.
func (s *Service) askerName(ctx context.Context, q *db.Queries, names map[string]string, id, speaker string) (string, error) {
	if speaker != "" {
		return speaker, nil
	}
	if name, ok := names[id]; ok {
		return name, nil
	}
	row, err := q.GetBot(ctx, id)
	if err != nil {
		return "", fmt.Errorf("get bot %s: %w", id, err)
	}
	names[id] = row.Name
	return row.Name, nil
}

// OpenQuestions lists the open questions of a chat, oldest first.
func (s *Service) OpenQuestions(ctx context.Context, chatID string) ([]OpenQuestion, error) {
	if _, err := s.GetBot(ctx, chatID); err != nil {
		return nil, err
	}
	return s.questionsOf(ctx, s.q, chatID, "")
}

// OpenQuestionsOf lists the open questions that a bot asked in a chat, for
// its prompt.
func (s *Service) OpenQuestionsOf(ctx context.Context, chatID, botID string) ([]OpenQuestion, error) {
	return s.questionsOf(ctx, s.q, chatID, botID)
}

// AllOpenQuestions lists every open question, grouped by chat and oldest
// first in each.
func (s *Service) AllOpenQuestions(ctx context.Context) ([]OpenQuestion, error) {
	rows, err := s.q.ListAllOpenQuestionEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the open questions: %w", err)
	}
	return s.toQuestions(ctx, s.q, rows, "")
}

// Waiting returns what waits for the user: open questions, pending
// approvals and paused goals.
func (s *Service) Waiting(ctx context.Context) (Waiting, error) {
	questions, err := s.AllOpenQuestions(ctx)
	if err != nil {
		return Waiting{}, err
	}
	approvals, err := s.q.ListBotsWithPendingApprovals(ctx)
	if err != nil {
		return Waiting{}, fmt.Errorf("list pending approvals: %w", err)
	}
	paused, err := s.q.ListTeamsWithPausedGoals(ctx)
	if err != nil {
		return Waiting{}, fmt.Errorf("list teams with paused goals: %w", err)
	}
	closed, err := s.closedQuestions(ctx)
	if err != nil {
		return Waiting{}, err
	}
	return Waiting{Questions: questions, Approvals: int64(len(approvals)), PausedGoals: int64(len(paused)), ClosedRecently: int64(len(closed)), Closed: closed}, nil
}

// closedQuestions lists the questions closed in the last 7 days, newest
// first, with how and when each closed.
func (s *Service) closedQuestions(ctx context.Context) ([]OpenQuestion, error) {
	rows, err := s.q.ListClosedQuestionsSince(ctx, s.nowMs()-closedWindowMs)
	if err != nil {
		return nil, fmt.Errorf("list closed questions: %w", err)
	}
	out, err := s.toQuestions(ctx, s.q, rows, "")
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		b, err := parseQuestion(row)
		if err != nil {
			return nil, err
		}
		out[i].State, out[i].ClosedAt = b.QuestionState, b.ClosedAt
	}
	return out, nil
}

// withQuestionState returns a question's body in state. answeredBy names the
// user's entry that answered it; closedAt is 0 for an open question.
func withQuestionState(raw, state, answeredBy string, closedAt int64) (string, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return "", fmt.Errorf("decode body: %w", err)
	}
	set := map[string]any{"questionState": state, "answeredBy": answeredBy, "closedAt": closedAt}
	for k, v := range set {
		if v == "" || v == int64(0) {
			delete(body, k)
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("encode %s: %w", k, err)
		}
		body[k] = b
	}
	out, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode body: %w", err)
	}
	return string(out), nil
}

// setQuestion stores a question in state with q and returns the changed entry.
func setQuestion(ctx context.Context, q *db.Queries, row db.Entry, state, answeredBy string, closedAt int64) (Entry, error) {
	body, err := withQuestionState(row.Body, state, answeredBy, closedAt)
	if err != nil {
		return Entry{}, fmt.Errorf("entry %s: %w", row.ID, err)
	}
	row, err = q.UpdateEntryBody(ctx, db.UpdateEntryBodyParams{Body: body, ID: row.ID})
	if err != nil {
		return Entry{}, fmt.Errorf("update entry %s: %w", row.ID, err)
	}
	return toEntry(row), nil
}

// publishQuestions shows changed questions and the attention they change.
func (s *Service) publishQuestions(entries []Entry) {
	for i := range entries {
		s.hub.Publish(Event{Type: "entry", BotID: entries[i].BotID, Entry: &entries[i]})
	}
	if len(entries) > 0 {
		s.hub.Publish(Event{Type: "bots"})
	}
}

// moveQuestion changes one question from state from to state to.
func (s *Service) moveQuestion(ctx context.Context, entryID, from, to string, check func(questionBody) error) (Entry, error) {
	var e Entry
	err := s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.GetEntry(ctx, entryID)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("entry", entryID)
		}
		if err != nil {
			return fmt.Errorf("get entry %s: %w", entryID, err)
		}
		b, err := parseQuestion(row)
		if err != nil {
			return err
		}
		if row.Kind != KindBot || b.QuestionState != from {
			return invalid("this message is not a %s question", from)
		}
		if err := check(b); err != nil {
			return err
		}
		var closedAt int64
		if to != QuestionOpen {
			closedAt = s.nowMs()
		}
		e, err = setQuestion(ctx, q, row, to, "", closedAt)
		return err
	})
	if err != nil {
		return Entry{}, err
	}
	s.publishQuestions([]Entry{e})
	return e, nil
}

// DismissQuestion closes an open question without an answer. Its bot gets
// no turn; its next prompt tells it once.
func (s *Service) DismissQuestion(ctx context.Context, entryID string) (Entry, error) {
	return s.moveQuestion(ctx, entryID, QuestionOpen, QuestionDismissed, func(questionBody) error { return nil })
}

// ReopenQuestion undoes a dismissal within 10 seconds.
func (s *Service) ReopenQuestion(ctx context.Context, entryID string) (Entry, error) {
	return s.moveQuestion(ctx, entryID, QuestionDismissed, QuestionOpen, func(b questionBody) error {
		if s.nowMs()-b.ClosedAt > reopenWindowMs {
			return invalid("a dismissal can be undone only within %d seconds", reopenWindowMs/1000)
		}
		return nil
	})
}

// WithdrawQuestions closes a bot's own open questions in a chat, named by
// their seq, because they are answered or no longer needed.
func (s *Service) WithdrawQuestions(ctx context.Context, botID, chatID string, seqs []int64) error {
	var changed []Entry
	err := s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListOpenQuestionEntries(ctx, chatID)
		if err != nil {
			return fmt.Errorf("list the open questions of %s: %w", chatID, err)
		}
		own, err := ownQuestions(rows, botID)
		if err != nil {
			return err
		}
		for _, seq := range seqs {
			row, ok := own[seq]
			if !ok {
				return invalid("#%d is not an open question of yours here", seq)
			}
			e, err := setQuestion(ctx, q, row, QuestionWithdrawn, "", s.nowMs())
			if err != nil {
				return err
			}
			changed = append(changed, e)
			delete(own, seq)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publishQuestions(changed)
	return nil
}

// ownQuestions maps the seq of each of the questions that botID asked.
func ownQuestions(rows []db.Entry, botID string) (map[int64]db.Entry, error) {
	own := map[int64]db.Entry{}
	for _, row := range rows {
		b, err := parseQuestion(row)
		if err != nil {
			return nil, err
		}
		if b.asker(row) == botID {
			own[row.Seq] = row
		}
	}
	return own, nil
}

// DismissalsSince lists a bot's questions in a chat that the user dismissed
// after the time after (ms), so that its prompt tells it once.
func (s *Service) DismissalsSince(ctx context.Context, chatID, botID string, after int64) ([]OpenQuestion, error) {
	rows, err := s.q.ListClosedQuestionEntries(ctx, db.ListClosedQuestionEntriesParams{BotID: chatID, State: QuestionDismissed, After: after})
	if err != nil {
		return nil, fmt.Errorf("list the dismissed questions of %s: %w", chatID, err)
	}
	return s.toQuestions(ctx, s.q, rows, botID)
}

// answered is what a user's message answers: the questions it closes, and
// in a room the members who asked them.
type answered struct {
	ids    []string
	rows   []db.Entry
	askers []string
}

// answersOf finds, with q, the questions of chat bot that a user's message
// answers: the named ones that are still open, or in a direct chat its one
// open question. A named question that closed meanwhile, or that is not of
// this chat, is skipped, so the message still goes out.
func answersOf(ctx context.Context, q *db.Queries, bot Bot, answers []string) (answered, error) {
	rows, err := q.ListOpenQuestionEntries(ctx, bot.ID)
	if err != nil {
		return answered{}, fmt.Errorf("list the open questions of %s: %w", bot.ID, err)
	}
	if len(answers) == 0 && !bot.IsRoom() && len(rows) == 1 {
		answers = []string{rows[0].ID}
	}
	var out answered
	for _, id := range slices.Compact(slices.Sorted(slices.Values(answers))) {
		i := slices.IndexFunc(rows, func(r db.Entry) bool { return r.ID == id })
		if i < 0 {
			continue
		}
		b, err := parseQuestion(rows[i])
		if err != nil {
			return answered{}, err
		}
		out.ids = append(out.ids, id)
		out.rows = append(out.rows, rows[i])
		out.askers = append(out.askers, b.asker(rows[i]))
	}
	return out, nil
}

// closeAnswered closes, with q, the questions that the user's entry
// userEntryID answers.
func (s *Service) closeAnswered(ctx context.Context, q *db.Queries, a answered, userEntryID string) ([]Entry, error) {
	var changed []Entry
	for _, row := range a.rows {
		e, err := setQuestion(ctx, q, row, QuestionAnswered, userEntryID, s.nowMs())
		if err != nil {
			return nil, err
		}
		changed = append(changed, e)
	}
	return changed, nil
}

// closeTaskQuestions closes the open questions about a team's task: its
// delivery, once the user accepted it, ended it or asked for changes, and
// a question to the user about it, once someone decided on it or it left
// the team's inbox (passed on, deferred or withdrawn).
func (s *Service) closeTaskQuestions(ctx context.Context, teamID, goalID string) error {
	var changed []Entry
	err := s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListOpenQuestionEntries(ctx, teamID)
		if err != nil {
			return fmt.Errorf("list the open questions of %s: %w", teamID, err)
		}
		var about answered
		for _, row := range rows {
			b, err := parseQuestion(row)
			if err != nil {
				return err
			}
			if b.GoalID == goalID {
				about.rows = append(about.rows, row)
			}
		}
		changed, err = s.closeAnswered(ctx, q, about, "")
		return err
	})
	if err != nil {
		return err
	}
	s.publishQuestions(changed)
	return nil
}
