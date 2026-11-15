package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func appendTexts(t *testing.T, svc *Service, botID, kind string, texts ...string) []Entry {
	t.Helper()
	var out []Entry
	for _, text := range texts {
		e, err := svc.AppendEntry(context.Background(), botID, kind, "", map[string]string{"text": text})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func idsOf(es []Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

// A search finds the chat's user, bot and agent messages with no case,
// newest first, and not its tool calls, events, approvals or other chats.
func TestSearchEntriesFindsTheChatsMessagesNewestFirst(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	bot, other := newTestBot(t, svc), newTestBot(t, svc)
	user := appendTexts(t, svc, bot.ID, KindUser, "Deploy the SERVER")
	appendTexts(t, svc, bot.ID, KindActivity, "server log")
	appendTexts(t, svc, bot.ID, KindEvent, "server stopped")
	appendTexts(t, svc, bot.ID, KindApproval, "allow server")
	reply := appendTexts(t, svc, bot.ID, KindBot, "The server is up.")
	agent := appendTexts(t, svc, bot.ID, KindAgent, "Сервер готов, server ok")
	appendTexts(t, svc, bot.ID, KindUser, "Thanks")
	appendTexts(t, svc, other.ID, KindUser, "other server")
	got, err := svc.SearchEntries(context.Background(), bot.ID, "Server")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{agent[0].ID, reply[0].ID, user[0].ID}
	if !slices.Equal(idsOf(got.Entries), want) || got.More {
		t.Fatalf("search = %v, more %v; want %v, more false", idsOf(got.Entries), got.More, want)
	}
	if got.Entries[0].Kind != KindAgent || !strings.Contains(string(got.Entries[0].Body), "Сервер") {
		t.Fatalf("first result = %+v; want the agent message with its body", got.Entries[0])
	}
	cyrillic, err := svc.SearchEntries(context.Background(), bot.ID, "СЕРВЕР")
	if err != nil || !slices.Equal(idsOf(cyrillic.Entries), []string{agent[0].ID}) {
		t.Fatalf("Cyrillic search = %v, %v; want the agent message", idsOf(cyrillic.Entries), err)
	}
}

func TestSearchEntriesReturnsAtMostFiftyNewest(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	texts := make([]string, MaxSearchResults)
	for i := range texts {
		texts[i] = fmt.Sprint("match ", i)
	}
	first := appendTexts(t, svc, bot.ID, KindUser, "match first")
	all := appendTexts(t, svc, bot.ID, KindUser, texts...)
	got, err := svc.SearchEntries(context.Background(), bot.ID, "match")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 50 || !got.More || got.Entries[0].ID != all[49].ID || got.Entries[49].ID != all[0].ID {
		t.Fatalf("51 matches gave %d results, more %v; want the 50 newest, more true", len(got.Entries), got.More)
	}
	if slices.Contains(idsOf(got.Entries), first[0].ID) {
		t.Fatal("the oldest match is in the results; want only the 50 newest")
	}
	exact, err := svc.SearchEntries(context.Background(), bot.ID, "match 1")
	if err != nil || len(exact.Entries) != 11 || exact.More {
		t.Fatalf(`"match 1" gave %d results, more %v, %v; want 11, more false`, len(exact.Entries), exact.More, err)
	}
}

func TestSearchEntriesAtFiftyMatchesSaysNoMore(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	texts := make([]string, MaxSearchResults)
	for i := range texts {
		texts[i] = "match"
	}
	appendTexts(t, svc, bot.ID, KindBot, texts...)
	got, err := svc.SearchEntries(context.Background(), bot.ID, "match")
	if err != nil || len(got.Entries) != 50 || got.More {
		t.Fatalf("50 matches gave %d results, more %v, %v; want 50, more false", len(got.Entries), got.More, err)
	}
}

func TestSearchEntriesNeedsTwoToTwoHundredCharacters(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	long := strings.Repeat("я", MaxSearchQuery)
	appendTexts(t, svc, bot.ID, KindUser, "ab", long)
	for _, q := range []string{"", "a", " a ", "я", long + "я"} {
		if _, err := svc.SearchEntries(context.Background(), bot.ID, q); !errors.Is(err, ErrInvalid) {
			t.Errorf("query of %d characters: err = %v; want ErrInvalid", len([]rune(q)), err)
		}
	}
	for _, q := range []string{"ab", " ab ", long} {
		if got, err := svc.SearchEntries(context.Background(), bot.ID, q); err != nil || len(got.Entries) != 1 {
			t.Errorf("query of %d characters: %d results, %v; want 1", len([]rune(q)), len(got.Entries), err)
		}
	}
}

// The query is text to find, never a pattern or SQL: %, _, quotes and
// backslashes match only themselves.
func TestSearchEntriesMatchesSpecialCharactersLiterally(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	es := appendTexts(t, svc, bot.ID, KindUser,
		"50% done", "500 done", "snake_case", "snakeXcase", `say "hi" and 'bye'`, `C:\temp`, `x'); DROP TABLE entries; --`)
	cases := []struct {
		query string
		want  []string
	}{
		{"0%", []string{es[0].ID}},
		{"e_c", []string{es[2].ID}},
		{`"hi"`, []string{es[4].ID}},
		{"'bye'", []string{es[4].ID}},
		{`:\t`, []string{es[5].ID}},
		{"%%", nil},
		{"__", nil},
		{"x'); DROP TABLE entries; --", []string{es[6].ID}},
		{"' OR '1'='1", nil},
	}
	for _, c := range cases {
		got, err := svc.SearchEntries(context.Background(), bot.ID, c.query)
		if err != nil || !slices.Equal(idsOf(got.Entries), append([]string{}, c.want...)) {
			t.Errorf("search %q = %v, %v; want %v", c.query, idsOf(got.Entries), err, c.want)
		}
	}
	all, err := svc.Entries(context.Background(), bot.ID, 0, 0)
	if err != nil || len(all) < len(es) {
		t.Fatalf("entries after the searches = %d, %v; want all %d kept", len(all), err, len(es))
	}
}
