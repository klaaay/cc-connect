package codex

import (
	"fmt"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// 慢消费者不能令最终正文和完成回执丢失，也不能堵住 RPC 读取循环。
func TestAppServerSession_FullEventQueuePreservesOrderedCompletion(t *testing.T) {
	s := &appServerSession{events: make(chan core.Event, 2), currentTurn: "turn-1"}
	defer s.closeEvents()
	emitted := make(chan struct{})
	go func() {
		for i := 0; i < 300; i++ {
			s.emit(core.Event{Type: core.EventText, Content: fmt.Sprint(i)})
		}
		s.completeTurn()
		close(emitted)
	}()
	select {
	case <-emitted:
	case <-time.After(time.Second):
		t.Fatal("producer blocked RPC reader")
	}
	for i := 0; i <= 300; i++ {
		select {
		case e := <-s.events:
			if i < 300 && (e.Type != core.EventText || e.Content != fmt.Sprint(i)) {
				t.Fatalf("event %d = %#v", i, e)
			}
			if i == 300 && (e.Type != core.EventResult || !e.Done) {
				t.Fatalf("missing completion: %#v", e)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d was lost", i)
		}
	}
}

func TestAppServerSession_CloseFullEventQueue(t *testing.T) {
	s := &appServerSession{events: make(chan core.Event, 1)}
	for i := 0; i < 300; i++ {
		s.emit(core.Event{Type: core.EventText, Content: "progress"})
	}
	done := make(chan struct{})
	go func() { s.closeEvents(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close blocked on slow consumer")
	}
	s.emit(core.Event{Type: core.EventResult, Done: true})
	for range s.events {
	}
}
