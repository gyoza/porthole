package source

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadLines(t *testing.T) {
	in := strings.NewReader("one\n{\"msg\":\"two\"}\n")
	ch := make(chan Event, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ReadLines(ctx, in, ReaderConfig{Name: "t", Pod: "pod"}, ch); err != nil {
		t.Fatal(err)
	}
	close(ch)
	var got []string
	for ev := range ch {
		if ev.Pod != "pod" {
			t.Fatalf("pod=%q", ev.Pod)
		}
		got = append(got, ev.Line)
	}
	if len(got) != 2 || got[0] != "one" || got[1] != `{"msg":"two"}` {
		t.Fatalf("got %#v", got)
	}
}

func TestSourceID(t *testing.T) {
	ev := Event{Namespace: "ns", Pod: "pod", Container: "c"}
	if ev.SourceID() != "ns/pod/c" {
		t.Fatalf("%s", ev.SourceID())
	}
	ev.Context = "prod1"
	if ev.SourceID() != "prod1/ns/pod/c" {
		t.Fatalf("%s", ev.SourceID())
	}
}

func TestHashColorStable(t *testing.T) {
	a := HashColorIndex("ns/pod", 10)
	b := HashColorIndex("ns/pod", 10)
	if a != b {
		t.Fatal("expected stable hash")
	}
}
