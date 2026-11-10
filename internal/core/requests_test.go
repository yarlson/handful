package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A request is a task now: the old request actions change nothing and say
// what to do instead.
func TestTheRequestActionsPointToTasks(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, _, _ := f.docsTeam(t)
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	for name, act := range map[string]func() (Item, error){
		"request":  func() (Item, error) { return f.svc.RequestWork(ctx, f.lead, docs.ID, item("Guide", "", "")) },
		"defer":    func() (Item, error) { return f.svc.DeferItem(ctx, f.lead, parse.ID, "Later.", RevisitManual, 0) },
		"undefer":  func() (Item, error) { return f.svc.UndeferItem(ctx, f.lead, parse.ID) },
		"withdraw": func() (Item, error) { return f.svc.WithdrawItem(ctx, Actor{Role: RoleUser}, parse.ID) },
	} {
		if _, err := act(); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "send_task") || !strings.Contains(err.Error(), "waits_for") {
			t.Errorf("%s: err = %v; want ErrInvalid that points to send_task with waits_for", name, err)
		}
	}
	board, err := f.svc.Board(ctx, docs.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Requests) != 0 || len(board.Asked) != 0 || board.Slots != (Slots{}) {
		t.Fatalf("Docs's board = requests %v, asked %v, slots %+v; want none", board.Requests, board.Asked, board.Slots)
	}
	if got := f.itemNow(t, parse.ID); got.State != ItemTodo || got.OwnerID != f.ana.ID {
		t.Fatalf("Parse = %+v; want it as it was", got)
	}
}
