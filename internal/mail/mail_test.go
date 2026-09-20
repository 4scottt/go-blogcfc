package mail

import (
	"context"
	"testing"
)

func TestRecorderKeepsMessagesInOrder(t *testing.T) {
	r := &Recorder{}
	_ = r.Send(context.Background(), Message{Subject: "one"})
	_ = r.Send(context.Background(), Message{Subject: "two"})
	got := r.Messages()
	if len(got) != 2 || got[0].Subject != "one" || got[1].Subject != "two" {
		t.Fatalf("recorded %+v", got)
	}
	r.Reset()
	if len(r.Messages()) != 0 {
		t.Fatal("reset did not clear")
	}
}

func TestLogSenderNeverFails(t *testing.T) {
	if err := (LogSender{}).Send(context.Background(), Message{To: []string{"a@example.com"}, Subject: "s"}); err != nil {
		t.Fatal(err)
	}
}
