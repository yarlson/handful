package core

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPostsRecordTheModelAndEffortOfTheirTurn(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	room, err := svc.CreateRoom(ctx, "Plan", "", []string{bots["Ana"].ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	meta := PostMeta{Model: "opus", ModelID: "claude-opus-5-5", Effort: "high"}
	if _, err := svc.PostToRoomWithMeta(ctx, room.ID, bots["Ana"].ID, "", "Done.", nil, 0, false, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, bots["Bo"].ID, "", "Old style.", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	direct, err := svc.AppendBotMessageWith(ctx, bots["Ana"].ID, "t1", "Hi.", nil, false, false, PostMeta{ModelID: "claude-opus-5-5"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := svc.Entries(ctx, room.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	for _, e := range append(entries, direct) {
		if e.Kind != KindBot {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(e.Body, &body); err != nil {
			t.Fatal(err)
		}
		got = append(got, map[string]any{"model": body["model"], "modelId": body["modelId"], "effort": body["effort"]})
	}
	want := []map[string]any{
		{"model": "opus", "modelId": "claude-opus-5-5", "effort": "high"},
		{"model": nil, "modelId": nil, "effort": nil},
		{"model": nil, "modelId": "claude-opus-5-5", "effort": nil},
	}
	if len(got) != len(want) {
		t.Fatalf("posts = %v; want %v", got, want)
	}
	for i := range want {
		for k, v := range want[i] {
			if got[i][k] != v {
				t.Errorf("post %d %s = %v; want %v", i, k, got[i][k], v)
			}
		}
	}
}
