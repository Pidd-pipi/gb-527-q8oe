package scheduler

import (
	"testing"
	"time"

	"satellite-contact-window-deconfliction/backend/internal/constants"
	"satellite-contact-window-deconfliction/backend/internal/model"
)

func newKeepTestGenerator() (*CandidateGenerator, time.Time) {
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	station := model.GroundStation{ID: 1, StationCode: "GS-2", AntennaCount: 2, SupportedBandsJSON: `["S"]`, StationStatus: "active"}
	assets := []model.SatelliteAsset{
		{ID: 1, SatelliteCode: "SAT-1", PriorityWeight: 1, SupportedBandsJSON: `["S"]`},
		{ID: 2, SatelliteCode: "SAT-2", PriorityWeight: 2, SupportedBandsJSON: `["S"]`},
		{ID: 3, SatelliteCode: "SAT-3", PriorityWeight: 1, SupportedBandsJSON: `["S"]`},
	}
	generator := NewCandidateGenerator(Weights{PriorityLoss: 4, MovementDistance: .02, ContactDuration: .003, ResourceMargin: 2}, []model.GroundStation{station}, assets, nil)
	return generator, base
}

func windowAt(id uint, base time.Time, satellite, priority int, duration time.Duration, locked bool) model.ContactWindow {
	return model.ContactWindow{ID: id, StationID: 1, SatelliteID: uint(satellite), StartAt: base, EndAt: base.Add(duration), Band: "S", Priority: priority, Locked: locked, Version: 1}
}

func keepHighOption(t *testing.T, generator *CandidateGenerator, group ConflictGroup) Suggestion {
	t.Helper()
	suggestions := generator.Generate(group)
	for _, suggestion := range suggestions {
		if suggestion.ActionType == "keep_high_priority" {
			return suggestion
		}
	}
	t.Fatal("keep_high_priority suggestion missing")
	return Suggestion{}
}

func TestKeepHighestUsesEveryAntennaChannel(t *testing.T) {
	generator, base := newKeepTestGenerator()
	// Station has two antennas and three overlapping windows; the two
	// highest-ranked windows must be kept and only the lowest queued for removal.
	windows := []model.ContactWindow{
		windowAt(1, base, 1, 5, 10*time.Minute, false),
		windowAt(2, base, 2, 5, 12*time.Minute, false), // priority 5 + weight 2 ranks first
		windowAt(3, base, 3, 1, 8*time.Minute, false),
	}
	group := newGroup(constants.ConflictTypeStationCapacity, windows, 2, 3, 0, "capacity", nil)
	option := keepHighOption(t, generator, group)

	assertIDSet(t, option.KeepWindowIDs, []uint{2, 1}, "kept")
	assertIDSet(t, option.MoveWindowIDs, []uint{3}, "moved")
	if option.KeptCount != 2 || option.MovedCount != 1 || option.AvailableChannels != 0 {
		t.Fatalf("expected kept=2 moved=1 available=0, got kept=%d moved=%d available=%d", option.KeptCount, option.MovedCount, option.AvailableChannels)
	}
	if option.RequiresManual {
		t.Fatal("fitting two open windows on two antennas must not require manual handling")
	}
}

func TestKeepHighestPlacesLockedWindowsBeforeFillingChannels(t *testing.T) {
	generator, base := newKeepTestGenerator()
	// Two antennas: one low-rank locked window must still occupy a channel;
	// the second channel goes to the highest-ranked open window.
	windows := []model.ContactWindow{
		windowAt(1, base, 1, 9, 10*time.Minute, false),
		windowAt(2, base, 2, 8, 10*time.Minute, false),
		windowAt(3, base, 3, 1, 10*time.Minute, true),
	}
	group := newGroup(constants.ConflictTypeStationCapacity, windows, 2, 3, 0, "capacity", nil)
	option := keepHighOption(t, generator, group)

	assertIDSet(t, option.KeepWindowIDs, []uint{3, 1}, "kept")
	assertIDSet(t, option.MoveWindowIDs, []uint{2}, "moved")
	if option.KeptCount != 2 || option.MovedCount != 1 || option.AvailableChannels != 0 {
		t.Fatalf("expected kept=2 moved=1 available=0, got kept=%d moved=%d available=%d", option.KeptCount, option.MovedCount, option.AvailableChannels)
	}
	if option.RequiresManual {
		t.Fatal("locked windows within channel count must not force manual handling")
	}
}

func TestKeepHighestRetainsAllLockedWindowsAndRoutesOverflowToManual(t *testing.T) {
	generator, base := newKeepTestGenerator()
	// Two antennas but three locked windows plus one open window: every locked
	// window is retained, the open window is queued for removal, and the
	// overflow requires manual planning.
	windows := []model.ContactWindow{
		windowAt(1, base, 1, 9, 10*time.Minute, true),
		windowAt(2, base, 2, 8, 10*time.Minute, true),
		windowAt(3, base, 3, 7, 10*time.Minute, true),
		windowAt(4, base, 1, 6, 10*time.Minute, false),
	}
	group := newGroup(constants.ConflictTypeStationCapacity, windows, 2, 4, 0, "capacity", nil)
	option := keepHighOption(t, generator, group)

	assertIDSet(t, option.KeepWindowIDs, []uint{1, 2, 3}, "kept")
	assertIDSet(t, option.MoveWindowIDs, []uint{4}, "moved")
	if !option.RequiresManual {
		t.Fatal("locked windows exceeding channel count must require manual handling")
	}
	for _, moved := range option.MoveWindowIDs {
		if moved == 1 || moved == 2 || moved == 3 {
			t.Fatalf("locked window %d must never be queued for removal", moved)
		}
	}

	all := generator.Generate(group)
	for _, suggestion := range all {
		if suggestion.ActionType != "keep_high_priority" {
			continue
		}
		if suggestion.KeptCount != 3 || suggestion.MovedCount != 1 {
			t.Fatalf("overflow counts mismatch: kept=%d moved=%d", suggestion.KeptCount, suggestion.MovedCount)
		}
		if suggestion.AvailableChannels != 0 {
			t.Fatalf("no channel can be free when locked windows overflow, got %d", suggestion.AvailableChannels)
		}
	}
}

func TestSuggestionsExposeReviewerCounts(t *testing.T) {
	generator, base := newKeepTestGenerator()
	windows := []model.ContactWindow{
		windowAt(1, base, 1, 5, 10*time.Minute, false),
		windowAt(2, base, 2, 4, 10*time.Minute, false),
	}
	group := newGroup(constants.ConflictTypeStationCapacity, windows, 2, 2, 0, "capacity", nil)
	for _, suggestion := range generator.Generate(group) {
		if suggestion.KeptCount != len(suggestion.KeepWindowIDs) {
			t.Fatalf("%s kept_count %d does not match %d ids", suggestion.ActionKey, suggestion.KeptCount, len(suggestion.KeepWindowIDs))
		}
		if suggestion.MovedCount != len(suggestion.MoveWindowIDs) {
			t.Fatalf("%s moved_count %d does not match %d ids", suggestion.ActionKey, suggestion.MovedCount, len(suggestion.MoveWindowIDs))
		}
		if suggestion.AvailableChannels < 0 || suggestion.AvailableChannels > 2 {
			t.Fatalf("%s reports impossible available channels %d", suggestion.ActionKey, suggestion.AvailableChannels)
		}
	}
}

func assertIDSet(t *testing.T, got, want []uint, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %s %v, got %v", label, want, got)
	}
	counts := map[uint]int{}
	for _, id := range got {
		counts[id]++
	}
	for _, id := range want {
		if counts[id] != 1 {
			t.Fatalf("expected %s set %v, got %v", label, want, got)
		}
	}
}
