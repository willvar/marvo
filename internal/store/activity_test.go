package store

import (
	"errors"
	"testing"
)

func TestActivityStorePublishesIdempotentlyAndTracksRead(t *testing.T) {
	state, _ := newTestStateDB(t)
	activities, err := NewActivityStore(state)
	if err != nil {
		t.Fatal(err)
	}
	input := ActivityPublish{
		Kind: ActivityKindNotice, Title: "研究完成", Content: "已整理结果。",
		SourceSessionID: "session-1", SourceMessageID: "message-1",
	}
	created, isNew, err := activities.Publish(input)
	if err != nil || !isNew || created.ID == "" || created.ReadAt != nil {
		t.Fatalf("Publish() = %#v, %t, %v", created, isNew, err)
	}
	duplicate, isNew, err := activities.Publish(input)
	if err != nil || isNew || duplicate.ID != created.ID {
		t.Fatalf("duplicate Publish() = %#v, %t, %v", duplicate, isNew, err)
	}
	page, err := activities.List(30, "")
	if err != nil || len(page.Activities) != 1 || page.Unread != 1 || page.Pending != 0 {
		t.Fatalf("List() = %#v, %v", page, err)
	}
	if err := activities.MarkRead([]string{created.ID}); err != nil {
		t.Fatal(err)
	}
	unread, pending, err := activities.Counts()
	if err != nil || unread != 0 || pending != 0 {
		t.Fatalf("Counts() = %d, %d, %v", unread, pending, err)
	}
}

func TestActivityStoreDeletesOneActivityIdempotently(t *testing.T) {
	state, _ := newTestStateDB(t)
	activities, _ := NewActivityStore(state)
	first, _, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindNotice, Title: "第一条", Content: "内容一",
		SourceSessionID: "session-1", SourceMessageID: "message-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindNotice, Title: "第二条", Content: "内容二",
		SourceSessionID: "session-2", SourceMessageID: "message-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := activities.Delete(first.ID)
	if err != nil || !deleted {
		t.Fatalf("Delete() = %t, %v", deleted, err)
	}
	if _, err := activities.Get(first.ID); !errors.Is(err, ErrActivityNotFound) {
		t.Fatalf("deleted Activity error = %v", err)
	}
	if _, err := activities.Get(second.ID); err != nil {
		t.Fatalf("unrelated Activity was deleted: %v", err)
	}
	deleted, err = activities.Delete(first.ID)
	if err != nil || deleted {
		t.Fatalf("second Delete() = %t, %v", deleted, err)
	}
	if _, err := activities.Delete("invalid"); !errors.Is(err, ErrInvalidActivity) {
		t.Fatalf("invalid Delete() error = %v", err)
	}
}

func TestActivityChoiceReplyReservationEnforcesSelectionMode(t *testing.T) {
	state, _ := newTestStateDB(t)
	activities, _ := NewActivityStore(state)
	single, _, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindChoice, Title: "选择方案", Content: "请选择。", Choices: []string{"A", "B"},
		SourceSessionID: "session-1", SourceMessageID: "message-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activities.BeginReply(single.ID, ActivityReply{Choices: []string{"A", "B"}, SessionID: "reply-invalid"}); !errors.Is(err, ErrInvalidActivity) {
		t.Fatalf("single-select accepted multiple choices: %v", err)
	}
	reserved, err := activities.BeginReply(single.ID, ActivityReply{Choices: []string{"A"}, SessionID: "reply-1"})
	if err != nil || !reserved.Replying || reserved.ReadAt == nil {
		t.Fatalf("BeginReply() = %#v, %v", reserved, err)
	}
	if _, err := activities.BeginReply(single.ID, ActivityReply{Text: "另一个回复", SessionID: "reply-2"}); !errors.Is(err, ErrActivityResponded) {
		t.Fatalf("concurrent reply error = %v", err)
	}
	if err := activities.CancelReply(single.ID, "reply-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := activities.BeginReply(single.ID, ActivityReply{Text: "我有其他想法", SessionID: "reply-2"}); err != nil {
		t.Fatal(err)
	}
	completed, err := activities.CompleteReply(single.ID, "reply-2")
	if err != nil || completed.RespondedAt == nil || completed.Replying || completed.ResponseText != "我有其他想法" {
		t.Fatalf("CompleteReply() = %#v, %v", completed, err)
	}
	if _, err := activities.BeginReply(single.ID, ActivityReply{Text: "重复", SessionID: "reply-3"}); !errors.Is(err, ErrActivityResponded) {
		t.Fatalf("completed reply error = %v", err)
	}

	multiple, _, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindChoice, Title: "多选", Content: "可多选。", Choices: []string{"A", "B"}, Multiple: true,
		SourceSessionID: "session-2", SourceMessageID: "message-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activities.BeginReply(multiple.ID, ActivityReply{Choices: []string{"A", "B"}, SessionID: "reply-many"}); err != nil {
		t.Fatalf("multi-select reply error = %v", err)
	}
}

func TestActivityStoreDetachesDeletedReplySession(t *testing.T) {
	state, _ := newTestStateDB(t)
	activities, _ := NewActivityStore(state)
	activity, _, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindNotice, Title: "结果", Content: "处理完成。",
		SourceSessionID: "source", SourceMessageID: "message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activities.BeginReply(activity.ID, ActivityReply{Text: "收到", SessionID: "reply-session"}); err != nil {
		t.Fatal(err)
	}
	if _, err := activities.CompleteReply(activity.ID, "reply-session"); err != nil {
		t.Fatal(err)
	}
	affected, err := activities.DetachReplySession("reply-session")
	if err != nil || affected != 1 {
		t.Fatalf("DetachReplySession() = %d, %v", affected, err)
	}
	detached, err := activities.Get(activity.ID)
	if err != nil || detached.ReplySessionID != "" || detached.RespondedAt == nil || detached.ResponseText != "收到" {
		t.Fatalf("detached Activity = %#v, %v", detached, err)
	}
	affected, err = activities.DetachReplySession("reply-session")
	if err != nil || affected != 0 {
		t.Fatalf("second DetachReplySession() = %d, %v", affected, err)
	}
}

func TestActivityStoreRejectsInvalidPublications(t *testing.T) {
	state, _ := newTestStateDB(t)
	activities, _ := NewActivityStore(state)
	for _, input := range []ActivityPublish{
		{Kind: "unknown", Title: "标题", Content: "内容", SourceSessionID: "s", SourceMessageID: "m"},
		{Kind: ActivityKindNotice, Title: "标题", Content: "内容", Choices: []string{"A"}, SourceSessionID: "s", SourceMessageID: "m"},
		{Kind: ActivityKindNotice, Title: "标题", Content: "内容", Multiple: true, SourceSessionID: "s", SourceMessageID: "m"},
		{Kind: ActivityKindChoice, Title: "标题", Content: "内容", Choices: []string{"A"}, SourceSessionID: "s", SourceMessageID: "m"},
		{Kind: ActivityKindNotice, Title: "", Content: "内容", SourceSessionID: "s", SourceMessageID: "m"},
	} {
		if _, _, err := activities.Publish(input); !errors.Is(err, ErrInvalidActivity) {
			t.Fatalf("Publish(%#v) error = %v", input, err)
		}
	}
}
