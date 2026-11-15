package core

import (
	"context"
	"errors"
	"testing"
)

func TestChatEntryFindsAnEntryOnlyInItsOwnChat(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	bot, other := newTestBot(t, svc), newTestBot(t, svc)
	post := appendTexts(t, svc, bot.ID, KindBot, "the release is out")[0]
	got, err := svc.ChatEntry(ctx, bot.ID, post.ID)
	if err != nil || got.ID != post.ID || got.BotID != bot.ID {
		t.Fatalf("ChatEntry(own chat) = %+v, %v; want %s", got, err, post.ID)
	}
	for name, ids := range map[string][2]string{
		"another chat":  {other.ID, post.ID},
		"a missing id":  {bot.ID, "missing"},
		"a missing bot": {"nobody", post.ID},
	} {
		got, err := svc.ChatEntry(ctx, ids[0], ids[1])
		if !errors.Is(err, ErrNotFound) || got.ID != "" {
			t.Errorf("%s: ChatEntry(%q, %q) = %+v, %v; want not found and no entry", name, ids[0], ids[1], got, err)
		}
	}
}
