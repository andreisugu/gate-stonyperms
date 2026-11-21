package engine

import (
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

func TestEngineVerboseRecordingAndFilters(t *testing.T) {
	e := New()
	u := model.NewUser(uuid.New(), "Larryy_")
	u.AddNode(model.NewNode("gate.command.server", true))
	groups := map[string]*model.Group{
		"default": model.NewGroup("default", 10),
	}

	e.StartVerboseRecording("Console", VerboseFilter{Query: "gate.*"})
	if !e.IsVerboseRecording() {
		t.Fatal("expected verbose recording to be active")
	}

	if got := e.PermissionValue(u, groups, "gate.command.server", nil); got != model.True {
		t.Fatalf("got %v, want True", got)
	}
	if got := e.PermissionValue(u, groups, "essentials.fly", nil); got != model.Undefined {
		t.Fatalf("got %v, want Undefined", got)
	}

	snap := e.StopVerboseRecording()
	if snap.Active {
		t.Fatal("expected recording to be stopped")
	}
	if snap.TotalCount != 1 || len(snap.Entries) != 1 {
		t.Fatalf("got count=%d len=%d, want 1/1", snap.TotalCount, len(snap.Entries))
	}
	if snap.Entries[0].Permission != "gate.command.server" || snap.Entries[0].Result != "true" {
		t.Fatalf("unexpected entry: %+v", snap.Entries[0])
	}

	snapAgain := e.SnapshotVerboseRecording()
	if snapAgain.TotalCount != 1 {
		t.Fatalf("expected stopped snapshot preserved, got %d", snapAgain.TotalCount)
	}
}

func TestEngineVerboseRecordingConcurrencyAndTruncation(t *testing.T) {
	e := New()
	u := model.NewUser(uuid.New(), "StressUser")
	groups := map[string]*model.Group{"default": model.NewGroup("default", 10)}

	e.StartVerboseRecording("Console", VerboseFilter{})
	const workers = 8
	const perWorker = 300
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_ = e.PermissionValue(u, groups, "stonyperms.check", nil)
			}
		}()
	}
	wg.Wait()

	snap := e.StopVerboseRecording()
	wantTotal := workers * perWorker
	if snap.TotalCount != wantTotal {
		t.Fatalf("TotalCount=%d, want %d", snap.TotalCount, wantTotal)
	}
	if !snap.Truncated || len(snap.Entries) != maxVerboseEntries {
		t.Fatalf("expected truncated=%v len=%d, got %v %d", true, maxVerboseEntries, snap.Truncated, len(snap.Entries))
	}
}
