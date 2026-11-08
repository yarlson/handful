package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// RoleBuilder marks Builder, the bot that sets up the user's bots, teams and
// rooms. Only Builder has the create_setup tool.
const RoleBuilder = "builder"

const (
	builderName         = "Builder"
	builderInstructions = "You help the user set up bots, teams and rooms in YarGPT, and explain how to use them."
	// BuilderWelcome is Builder's first message. Builder's prompt quotes it,
	// because the message is not in Builder's Claude session.
	BuilderWelcome = `Hi! I set up bots, teams and rooms for you. Tell me what you want help with, or pick a starting point:

- **Personal assistant**: one bot for your day: plans, reminders, research and drafts.
- **Software team**: a lead, a developer and a reviewer who change your code and check it with tests.
- **Research desk**: a lead, a researcher and a fact-checker who answer questions with sources.
- **Writing studio**: an editor and a writer who plan, draft and edit your texts.

Or describe your own idea. I ask a few questions and suggest a setup, and I create nothing until you confirm it.`
)

// SeedBuilder creates Builder with its welcome message when the database holds
// no bot and no room, as on the first start. Builder starts without a turn: the
// welcome waits for the user's answer.
func (s *Service) SeedBuilder(ctx context.Context) error {
	bots, err := s.q.ListBots(ctx)
	if err != nil {
		return fmt.Errorf("list bots: %w", err)
	}
	if len(bots) > 0 {
		return nil
	}
	id := uuid.NewString()
	if err := s.prepareWorkspace(s.Workspace(id)); err != nil {
		return err
	}
	err = s.inTx(ctx, func(q *db.Queries) error { return insertBuilder(ctx, q, id, s.nowMs()) })
	if err != nil {
		return errors.Join(err, os.RemoveAll(filepath.Join(s.dataDir, "bots", id)))
	}
	s.hub.Publish(Event{Type: "bots"})
	return nil
}

func insertBuilder(ctx context.Context, q *db.Queries, id string, now int64) error {
	if _, err := q.CreateBot(ctx, db.CreateBotParams{ID: id, Name: builderName, Instructions: builderInstructions, Kind: kindAgentBot, CreatedAt: now, UpdatedAt: now}); err != nil {
		return fmt.Errorf("create %s: %w", builderName, err)
	}
	if err := q.SetBotRole(ctx, db.SetBotRoleParams{Role: RoleBuilder, ID: id}); err != nil {
		return fmt.Errorf("mark %s as the builder: %w", builderName, err)
	}
	body, err := json.Marshal(map[string]any{"text": BuilderWelcome, "fallback": false, "needsReply": true})
	if err != nil {
		return fmt.Errorf("encode the welcome of %s: %w", builderName, err)
	}
	if _, err := q.AppendEntry(ctx, db.AppendEntryParams{BotID: id, ID: uuid.NewString(), Kind: KindBot, Body: string(body), CreatedAt: now}); err != nil {
		return fmt.Errorf("add the welcome of %s: %w", builderName, err)
	}
	return nil
}
