package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// questionOf reads the question fields of an entry.
func questionOf(t *testing.T, svc *Service, id string) (state, answeredBy string, closedAt int64) {
	t.Helper()
	e, err := svc.GetEntry(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		QuestionState string `json:"questionState"`
		AnsweredBy    string `json:"answeredBy"`
		ClosedAt      int64  `json:"closedAt"`
	}
	if err := json.Unmarshal(e.Body, &b); err != nil {
		t.Fatal(err)
	}
	return b.QuestionState, b.AnsweredBy, b.ClosedAt
}

func ask(t *testing.T, svc *Service, botID, text, question string) Entry {
	t.Helper()
	e, err := svc.AppendBotMessageWith(context.Background(), botID, "t1", text, nil, false, true, PostMeta{Question: question})
	if err != nil {
		t.Fatalf("ask %q: %v", text, err)
	}
	return e
}

func openCount(t *testing.T, svc *Service, id string) int64 {
	t.Helper()
	bots, err := svc.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		if b.ID == id {
			return b.OpenQuestions
		}
	}
	t.Fatalf("bot %s not listed", id)
	return 0
}

// lastUserEntry returns the user's latest message in a chat.
func lastUserEntry(t *testing.T, svc *Service, id string) Entry {
	t.Helper()
	entries, err := svc.Entries(context.Background(), id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var last Entry
	for _, e := range entries {
		if e.Kind == KindUser && e.Seq > last.Seq {
			last = e
		}
	}
	return last
}

func summaries(qs []OpenQuestion) []string {
	out := []string{}
	for _, q := range qs {
		out = append(out, q.AskerName+": "+q.Question)
	}
	return out
}

func TestARoomQuestionStaysOpenAfterLaterPosts(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoomWithMeta(ctx, room.ID, bots["Ana"].ID, "t1", "Which CRM?\nHubSpot or Pipedrive.", nil, 0, true, PostMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Ana"].ID, "t1", "More context.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Bo"].ID, "t2", "I agree.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, room.ID, "Thinking.", "n1"); err != nil {
		t.Fatal(err)
	}
	open, err := svc.OpenQuestions(ctx, room.ID)
	if err != nil || !slices.Equal(summaries(open), []string{"Ana: Which CRM?"}) || open[0].AskerID != bots["Ana"].ID || open[0].ChatID != room.ID {
		t.Fatalf("open questions = %+v, %v; want Ana's question, still open", open, err)
	}
	if got := attentionOf(t, svc, room.ID); got != AttentionQuestion {
		t.Fatalf("room attention = %q; want %q", got, AttentionQuestion)
	}
	if _, err := svc.OpenQuestions(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("questions of a missing chat: err = %v; want ErrNotFound", err)
	}
}

func TestAPlainReplyAnswersTheOnlyOpenQuestionOfADirectChat(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ana := newAgents(t, svc, "Ana")["Ana"]
	repo := ask(t, svc, ana.ID, "Which repo should I use?", "Which repo?")
	if _, _, err := svc.SubmitUserMessage(ctx, ana.ID, "Use yarstack.", "n1"); err != nil {
		t.Fatal(err)
	}
	reply := lastUserEntry(t, svc, ana.ID)
	var body userBody
	if err := json.Unmarshal(reply.Body, &body); err != nil || reply.Kind != KindUser || !slices.Equal(body.Answers, []string{repo.ID}) {
		t.Fatalf("user entry = %s, %v; want it to answer the question", reply.Body, err)
	}
	if state, by, at := questionOf(t, svc, repo.ID); state != QuestionAnswered || by != reply.ID || at == 0 {
		t.Fatalf("question = %s by %s at %d; want answered by the reply", state, by, at)
	}
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after the answer = %q; want none", got)
	}

	first, second := ask(t, svc, ana.ID, "Which branch?", ""), ask(t, svc, ana.ID, "Which name?", "")
	if n := openCount(t, svc, ana.ID); n != 2 {
		t.Fatalf("open questions = %d; want 2", n)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, ana.ID, "main", "n2"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []Entry{first, second} {
		if state, _, _ := questionOf(t, svc, q.ID); state != QuestionOpen {
			t.Fatalf("question %s = %s after a plain reply to two; want open", q.ID, state)
		}
	}
}

func TestAnAnswerClosesTheNamedQuestionAndWakesItsAskerInARoom(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo", "Cy")
	ana := bots["Ana"]
	first, second := ask(t, svc, ana.ID, "Which branch?", ""), ask(t, svc, ana.ID, "Which name?", "")
	// An answer to a question that is gone or closed is skipped: the message
	// goes out, answers nothing, and closes no other open question.
	if _, _, err := svc.SubmitAnswer(ctx, ana.ID, "x", "n0", []string{"missing"}); err != nil {
		t.Fatalf("answer a missing question: %v; want the message sent", err)
	}
	if n := openCount(t, svc, ana.ID); n != 2 {
		t.Fatalf("open questions after answering a missing one = %d; want 2", n)
	}
	var skipped struct {
		Answers []string `json:"answers"`
	}
	if err := json.Unmarshal(lastUserEntry(t, svc, ana.ID).Body, &skipped); err != nil || len(skipped.Answers) != 0 {
		t.Fatalf("answers of a message to a missing question = %v, %v; want none", skipped.Answers, err)
	}
	if _, _, err := svc.SubmitAnswer(ctx, ana.ID, "Call it yarstack.", "n1", []string{second.ID, second.ID}); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := questionOf(t, svc, first.ID); state != QuestionOpen {
		t.Fatalf("the other question = %s; want open", state)
	}
	reply := lastUserEntry(t, svc, ana.ID)
	if state, by, _ := questionOf(t, svc, second.ID); state != QuestionAnswered || by != reply.ID {
		t.Fatalf("the answered question = %s by %s; want answered by %s", state, by, reply.ID)
	}
	_, _, err := svc.SubmitAnswer(ctx, ana.ID, "Again.", "n2", []string{second.ID})
	if err != nil {
		t.Fatalf("answer a closed question: %v; want the message sent", err)
	}
	if state, by, _ := questionOf(t, svc, second.ID); state != QuestionAnswered || by != reply.ID || lastUserEntry(t, svc, ana.ID).ID == reply.ID {
		t.Fatalf("the closed question = %s by %s; want it still answered by the first reply %s", state, by, reply.ID)
	}
	drainQueue(t, svc)

	room, err := svc.CreateRoom(ctx, "Plan", "", []string{ana.ID, bots["Bo"].ID, bots["Cy"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Bo"].ID, "t1", "Ship today?", nil, 0, true); err != nil {
		t.Fatal(err)
	}
	open, err := svc.OpenQuestions(ctx, room.ID)
	if err != nil || len(open) != 1 {
		t.Fatalf("room questions = %+v, %v", open, err)
	}
	drainQueue(t, svc)
	if _, _, err := svc.SubmitAnswer(ctx, room.ID, "Yes. @Cy please help.", "n3", []string{open[0].EntryID}); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Bo@room", "Cy@room"}) && !equal(got, []string{"Cy@room", "Bo@room"}) {
		t.Fatalf("queued after the answer = %q; want the asker and the mentioned member", got)
	}
	drainQueue(t, svc)
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Bo"].ID, "t1", "And tomorrow?", nil, 0, true); err != nil {
		t.Fatal(err)
	}
	open, _ = svc.OpenQuestions(ctx, room.ID)
	if _, _, err := svc.SubmitAnswer(ctx, room.ID, "No.", "n4", []string{open[0].EntryID}); err != nil {
		t.Fatal(err)
	}
	if got := queued(t, svc, bots); !equal(got, []string{"Bo@room"}) {
		t.Fatalf("queued after an answer without mentions = %q; want only the asker", got)
	}
}

func TestTheUserDismissesAQuestionAndCanUndoItBriefly(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	ana := bots["Ana"]
	frozen := svc.now()
	svc.now = func() time.Time { return frozen }
	q := ask(t, svc, ana.ID, "May I sign up?", "")
	before := svc.nowMs() - 1
	if _, err := svc.ReopenQuestion(ctx, q.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reopen an open question: err = %v; want ErrInvalid", err)
	}
	e, err := svc.DismissQuestion(ctx, q.ID)
	if err != nil || e.Rev <= q.Rev {
		t.Fatalf("dismiss = %+v, %v; want a new rev", e, err)
	}
	if state, _, at := questionOf(t, svc, q.ID); state != QuestionDismissed || at != frozen.UnixMilli() {
		t.Fatalf("question = %s at %d; want dismissed now", state, at)
	}
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after a dismissal = %q; want none", got)
	}
	if _, err := svc.DismissQuestion(ctx, q.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("dismiss twice: err = %v; want ErrInvalid", err)
	}
	gone, err := svc.DismissalsSince(ctx, ana.ID, ana.ID, before)
	if err != nil || !slices.Equal(summaries(gone), []string{"Ana: May I sign up?"}) {
		t.Fatalf("dismissals = %+v, %v", gone, err)
	}
	if gone, _ := svc.DismissalsSince(ctx, ana.ID, ana.ID, frozen.UnixMilli()); len(gone) != 0 {
		t.Fatalf("dismissals after the dismissal = %+v; want none", gone)
	}
	if gone, _ := svc.DismissalsSince(ctx, ana.ID, bots["Bo"].ID, before); len(gone) != 0 {
		t.Fatalf("another bot's dismissals = %+v; want none", gone)
	}
	svc.now = func() time.Time { return frozen.Add(10 * time.Second) }
	if _, err := svc.ReopenQuestion(ctx, q.ID); err != nil {
		t.Fatalf("reopen at 10 s: %v", err)
	}
	if state, _, at := questionOf(t, svc, q.ID); state != QuestionOpen || at != 0 {
		t.Fatalf("question = %s at %d; want open again", state, at)
	}
	if _, err := svc.DismissQuestion(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return frozen.Add(20*time.Second + time.Millisecond) }
	if _, err := svc.ReopenQuestion(ctx, q.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "within 10 seconds") {
		t.Fatalf("reopen after 10 s: err = %v; want ErrInvalid that names 10 seconds", err)
	}
	plain, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "Done.", nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DismissQuestion(ctx, plain.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("dismiss a plain message: err = %v; want ErrInvalid", err)
	}
	if _, err := svc.DismissQuestion(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dismiss a missing entry: err = %v; want ErrNotFound", err)
	}
}

func TestABotWithdrawsOnlyItsOwnOpenQuestions(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ bot, text string }{{"Ana", "Ship today?"}, {"Bo", "Which CRM?"}, {"Ana", "Who reviews?"}} {
		if _, err := svc.PostToRoom(ctx, room.ID, bots[c.bot].ID, "t1", c.text, nil, 0, true); err != nil {
			t.Fatal(err)
		}
	}
	open, _ := svc.OpenQuestions(ctx, room.ID)
	ana1, bo, ana2 := open[0].Seq, open[1].Seq, open[2].Seq
	err = svc.WithdrawQuestions(ctx, bots["Ana"].ID, room.ID, []int64{ana1, bo})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "#"+itoa(bo)+" ") {
		t.Fatalf("withdraw another bot's question: err = %v; want ErrInvalid naming #%d", err, bo)
	}
	if left, _ := svc.OpenQuestions(ctx, room.ID); len(left) != 3 {
		t.Fatalf("open after a refused withdrawal = %d; want 3, nothing withdrawn", len(left))
	}
	if err := svc.WithdrawQuestions(ctx, bots["Ana"].ID, room.ID, []int64{ana1, ana1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("withdraw one question twice: err = %v; want ErrInvalid", err)
	}
	if err := svc.WithdrawQuestions(ctx, bots["Ana"].ID, room.ID, []int64{ana2, ana1}); err != nil {
		t.Fatal(err)
	}
	left, _ := svc.OpenQuestions(ctx, room.ID)
	if !slices.Equal(summaries(left), []string{"Bo: Which CRM?"}) {
		t.Fatalf("open after the withdrawal = %q; want only Bo's", summaries(left))
	}
	if state, _, at := questionOf(t, svc, open[0].EntryID); state != QuestionWithdrawn || at == 0 {
		t.Fatalf("withdrawn question = %s at %d", state, at)
	}
	if err := svc.WithdrawQuestions(ctx, bots["Ana"].ID, room.ID, []int64{999}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("withdraw a missing seq: err = %v; want ErrInvalid", err)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestABotHasAtMostTwentyOpenQuestionsInAChat(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	ana := bots["Ana"]
	if _, err := svc.AppendBotMessageWith(ctx, ana.ID, "t1", "Hi.", nil, false, false, PostMeta{Question: "Hi?"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a question line without needs_reply: err = %v; want ErrInvalid", err)
	}
	if _, err := svc.AppendBotMessageWith(ctx, ana.ID, "t1", "Long?", nil, false, true, PostMeta{Question: strings.Repeat("q", maxQuestionLen+1)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a question line over 140 characters: err = %v; want ErrInvalid", err)
	}
	ask(t, svc, ana.ID, "Longest?", strings.Repeat("q", maxQuestionLen))
	for i := 1; i < MaxOpenQuestions; i++ {
		ask(t, svc, ana.ID, "Question?", "")
	}
	_, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "One more?", nil, false, true)
	if err == nil || err.Error() != "invalid input: You have 20 open questions here; close some with closes_questions first." {
		t.Fatalf("the 21st question: err = %v; want the limit", err)
	}
	if _, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "A plain message is fine.", nil, false, false); err != nil {
		t.Fatalf("a plain message at the limit: %v", err)
	}
	if n := openCount(t, svc, ana.ID); n != MaxOpenQuestions {
		t.Fatalf("open questions = %d; want %d", n, MaxOpenQuestions)
	}

	room, err := svc.CreateRoom(ctx, "Plan", "", []string{ana.ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoomWithMeta(ctx, room.ID, ana.ID, "t1", "Hi.", nil, 0, false, PostMeta{Question: "Hi?"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a room question line without needs_reply: err = %v; want ErrInvalid", err)
	}
	for range MaxOpenQuestions {
		if _, err := svc.PostToRoomWithMeta(ctx, room.ID, ana.ID, "t1", "Room question?", nil, 0, true, PostMeta{Question: "Ship?"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.PostToRoom(ctx, room.ID, ana.ID, "t1", "One more?", nil, 0, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Ana's 21st room question: err = %v; want ErrInvalid", err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Bo"].ID, "t1", "Bo asks?", nil, 0, true); err != nil {
		t.Fatalf("another member's question: %v", err)
	}
	mine, _ := svc.OpenQuestionsOf(ctx, room.ID, ana.ID)
	if len(mine) != MaxOpenQuestions || mine[0].Question != "Ship?" {
		t.Fatalf("Ana's open questions = %d, %+v", len(mine), mine[0])
	}
}

func TestAnsweringTheGoalClosesTheDeliveryQuestion(t *testing.T) {
	for name, act := range map[string]func(*boardFixture) error{
		"accept": func(f *boardFixture) error {
			_, err := f.svc.AcceptGoal(context.Background(), f.goal.ID, true)
			return err
		},
		"end": func(f *boardFixture) error { return f.svc.EndGoal(context.Background(), f.goal.ID, true) },
		"changes": func(f *boardFixture) error {
			_, err := f.svc.CreateItem(context.Background(), f.lead, item("Fix", f.ana.ID, ""))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newBoardFixture(t, 30)
			ctx := context.Background()
			parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
			f.finish(t, parse, f.ana)
			other, err := f.svc.PostToRoom(ctx, f.team.ID, f.ana.ID, "t1", "Unrelated?", nil, 0, true)
			if err != nil || other != nil {
				t.Fatal(err)
			}
			if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
				t.Fatal(err)
			}
			if open, _ := f.svc.OpenQuestions(ctx, f.team.ID); len(open) != 2 {
				t.Fatalf("open after delivery = %+v; want the delivery and Ana's", open)
			}
			if err := act(f); err != nil {
				t.Fatal(err)
			}
			open, _ := f.svc.OpenQuestions(ctx, f.team.ID)
			if !slices.Equal(summaries(open), []string{"Ana: Unrelated?"}) {
				t.Fatalf("open after %s = %q; want only Ana's", name, summaries(open))
			}
		})
	}
}

func TestWaitingListsWhatWaitsForTheUser(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	empty, err := svc.Waiting(ctx)
	if err != nil || len(empty.Questions) != 0 || empty.Approvals != 0 || empty.PausedGoals != 0 || empty.ClosedRecently != 0 || empty.Closed == nil || len(empty.Closed) != 0 {
		t.Fatalf("waiting with nothing = %+v, %v", empty, err)
	}
	ask(t, svc, bots["Ana"].ID, "First?", "")
	ask(t, svc, bots["Bo"].ID, "Bo's?", "")
	old := ask(t, svc, bots["Ana"].ID, "Second?", "")
	if _, err := svc.DismissQuestion(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	all, err := svc.AllOpenQuestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byChat := map[string][]string{}
	for _, q := range all {
		byChat[q.ChatID] = append(byChat[q.ChatID], q.Question)
	}
	if !slices.Equal(byChat[bots["Ana"].ID], []string{"First?"}) || !slices.Equal(byChat[bots["Bo"].ID], []string{"Bo's?"}) {
		t.Fatalf("open questions by chat = %v", byChat)
	}
	w, err := svc.Waiting(ctx)
	if err != nil || len(w.Questions) != 2 || w.ClosedRecently != 1 {
		t.Fatalf("waiting = %+v, %v; want 2 questions and 1 closed", w, err)
	}
	if len(w.Closed) != 1 || w.Closed[0].EntryID != old.ID || w.Closed[0].Question != "Second?" || w.Closed[0].AskerName != "Ana" ||
		w.Closed[0].State != QuestionDismissed || w.Closed[0].ClosedAt == 0 || w.Questions[0].State != "" {
		t.Fatalf("closed = %+v; want the dismissed question with its state and time, and no state on open ones", w.Closed)
	}
	later := svc.now().Add(time.Minute)
	svc.now = func() time.Time { return later }
	first := byChatEntry(t, all, "Bo's?")
	if _, err := svc.DismissQuestion(ctx, first); err != nil {
		t.Fatal(err)
	}
	if w, _ := svc.Waiting(ctx); len(w.Closed) != 2 || w.Closed[0].EntryID != first || w.Closed[1].EntryID != old.ID {
		t.Fatalf("closed = %+v; want the newest first", w.Closed)
	}
	svc.now = func() time.Time { return later.Add(7*24*time.Hour + time.Minute) }
	if w, _ := svc.Waiting(ctx); w.ClosedRecently != 0 || len(w.Closed) != 0 {
		t.Fatalf("closed a week later = %d, %+v; want none", w.ClosedRecently, w.Closed)
	}

	f := newBoardFixture(t, 1)
	idea := f.create(t, f.lead, item("Idea", f.ana.ID, ""))
	f.start(t, idea, f.ana)
	if err := f.svc.StopTeam(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	if w, err := f.svc.Waiting(ctx); err != nil || w.PausedGoals != 1 {
		t.Fatalf("waiting with a paused goal = %+v, %v", w, err)
	}
}

// published collects the entry and bots events that act publishes.
func published(t *testing.T, svc *Service, act func()) (entries []string, bots int) {
	t.Helper()
	events, stop := svc.Hub().Subscribe()
	defer stop()
	act()
	for {
		select {
		case e := <-events:
			if e.Type == "entry" {
				entries = append(entries, e.Entry.ID)
			}
			if e.Type == "bots" {
				bots++
			}
		default:
			return entries, bots
		}
	}
}

func TestClosingQuestionsShowsEachChange(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ana := newAgents(t, svc, "Ana")["Ana"]
	first, second := ask(t, svc, ana.ID, "One?", ""), ask(t, svc, ana.ID, "Two?", "")
	entries, bots := published(t, svc, func() {
		if _, _, err := svc.SubmitAnswer(ctx, ana.ID, "Both.", "n1", []string{second.ID, first.ID}); err != nil {
			t.Fatal(err)
		}
	})
	if !slices.Contains(entries, first.ID) || !slices.Contains(entries, second.ID) || bots == 0 {
		t.Fatalf("published %q and %d bots events; want both questions and the attention", entries, bots)
	}
	for _, q := range []Entry{first, second} {
		if state, _, _ := questionOf(t, svc, q.ID); state != QuestionAnswered {
			t.Fatalf("question %s = %s; want answered", q.ID, state)
		}
	}
	third := ask(t, svc, ana.ID, "Three?", "")
	if entries, bots := published(t, svc, func() {
		if err := svc.WithdrawQuestions(ctx, ana.ID, ana.ID, nil); err != nil {
			t.Fatal(err)
		}
	}); len(entries) != 0 || bots != 0 {
		t.Fatalf("an empty withdrawal published %q and %d bots events; want nothing", entries, bots)
	}
	if entries, bots := published(t, svc, func() {
		if err := svc.WithdrawQuestions(ctx, ana.ID, ana.ID, []int64{third.Seq}); err != nil {
			t.Fatal(err)
		}
	}); !slices.Equal(entries, []string{third.ID}) || bots != 1 {
		t.Fatalf("a withdrawal published %q and %d bots events", entries, bots)
	}
}

func TestEndingAGoalShowsItsDeliveryClosed(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	f.finish(t, parse, f.ana)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	open, _ := f.svc.OpenQuestions(ctx, f.team.ID)
	entries, _ := published(t, f.svc, func() {
		if err := f.svc.EndGoal(ctx, f.goal.ID, true); err != nil {
			t.Fatal(err)
		}
	})
	if len(open) != 1 || !slices.Contains(entries, open[0].EntryID) {
		t.Fatalf("published %q; want the delivery %+v closed", entries, open)
	}
}

// byChatEntry returns the entry id of the question with summary q.
func byChatEntry(t *testing.T, qs []OpenQuestion, q string) string {
	t.Helper()
	for _, x := range qs {
		if x.Question == q {
			return x.EntryID
		}
	}
	t.Fatalf("no question %q in %+v", q, qs)
	return ""
}
