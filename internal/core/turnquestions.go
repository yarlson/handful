package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"handful/internal/db"
)

// TurnAnswers lists the questions that the user's message of a turn in a
// direct chat answers, so the bot's prompt can say which one it answers.
func (s *Service) TurnAnswers(ctx context.Context, botID, turnID string) ([]OpenQuestion, error) {
	raw, err := s.q.GetTurnUserEntryBody(ctx, db.GetTurnUserEntryBodyParams{BotID: botID, TurnID: turnID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the user message of turn %s: %w", turnID, err)
	}
	var body userBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return nil, fmt.Errorf("decode the user message of turn %s: %w", turnID, err)
	}
	var rows []db.Entry
	for _, id := range body.Answers {
		row, err := s.q.GetEntry(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue // deleted since; the turn goes on without it
		}
		if err != nil {
			return nil, fmt.Errorf("read question %s: %w", id, err)
		}
		rows = append(rows, row)
	}
	return s.toQuestions(ctx, s.q, rows, "")
}

// PreviousTurnStart is when the bot's latest turn in a chat (roomID "" for
// its direct chat) before turnID started, in ms; 0 when it had none.
func (s *Service) PreviousTurnStart(ctx context.Context, botID, roomID, turnID string) (int64, error) {
	at, err := s.q.PreviousTurnStart(ctx, db.PreviousTurnStartParams{BotID: botID, RoomID: roomID, ID: turnID})
	if err != nil {
		return 0, fmt.Errorf("find the previous turn of bot %s: %w", botID, err)
	}
	return at, nil
}
