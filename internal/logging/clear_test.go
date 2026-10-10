package logging

import (
	"bytes"
	"sync"
	"testing"
)

func TestClearPreservesCursorAndExternalLogs(t *testing.T) {
	var output bytes.Buffer
	log := New(&output)
	for i := 0; i < Capacity+2; i++ {
		log.Info("Before clear")
	}
	cleared := Clear(log.With("mailbox_id", "box"))
	if cleared != Capacity+2 {
		t.Fatal(cleared)
	}
	page := Read(log, Query{})
	if len(page.Entries) != 0 || page.Next != cleared || page.ClearedThrough != cleared {
		t.Fatal(page)
	}
	if !bytes.Contains(output.Bytes(), []byte("Before clear")) {
		t.Fatal("external logs removed")
	}
	log.Info("After clear")
	page = Read(log, Query{After: cleared})
	if len(page.Entries) != 1 || page.Entries[0].Message != "After clear" || page.Next != cleared+1 {
		t.Fatal(page)
	}
}

func TestConcurrentClearAndWriteRetainOnlyNewerEntries(t *testing.T) {
	var output bytes.Buffer
	log := New(&output)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				log.Info("Concurrent event")
				Clear(log)
				Read(log, Query{})
			}
		}()
	}
	wg.Wait()
	floor := Clear(log)
	log.Info("Final event")
	page := Read(log, Query{})
	if len(page.Entries) != 1 || page.Entries[0].Seq != floor+1 || page.ClearedThrough != floor {
		t.Fatal(page)
	}
}
