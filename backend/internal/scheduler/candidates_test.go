package scheduler

import (
	"testing"
	"time"

	"satellite-contact-window-deconfliction/backend/internal/constants"
	"satellite-contact-window-deconfliction/backend/internal/model"
)

func keepSuggestion(t *testing.T, suggestions []Suggestion) Suggestion {
	t.Helper()
	for _, suggestion := range suggestions {
		if suggestion.ActionType == "keep_high_priority" {
			return suggestion
		}
	}
	t.Fatal("missing keep_high_priority suggestion")
	return Suggestion{}
}

func assertIDs(t *testing.T, label string, got, want []uint) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
	}
}

func TestKeepHighestFillsRemainingChannels(t *testing.T) {
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	station := model.GroundStation{ID: 1, StationCode: "GS-2CH", AntennaCount: 2, SupportedBandsJSON: `["S"]`, StationStatus: "active"}
	assets := []model.SatelliteAsset{
		{ID: 1, SatelliteCode: "SAT-1", PriorityWeight: 1, SupportedBandsJSON: `["S"]`},
		{ID: 2, SatelliteCode: "SAT-2", PriorityWeight: 0, SupportedBandsJSON: `["S"]`},
	}
	windows := []model.ContactWindow{
		{ID: 11, StationID: 1, SatelliteID: 1, StartAt: base, EndAt: base.Add(10 * time.Minute), Band: "S", Priority: 5, Version: 1},
		{ID: 12, StationID: 1, SatelliteID: 2, StartAt: base.Add(time.Minute), EndAt: base.Add(9 * time.Minute), Band: "S", Priority: 4, Version: 1},
		{ID: 13, StationID: 1, SatelliteID: 1, StartAt: base.Add(2 * time.Minute), EndAt: base.Add(8 * time.Minute), Band: "S", Priority: 1, Version: 1},
	}
	group := newGroup(constants.ConflictTypeStationCapacity, windows, station.AntennaCount, 3, 0, "capacity", nil)
	generator := NewCandidateGenerator(Weights{PriorityLoss: 4, MovementDistance: .02, ContactDuration: .003, ResourceMargin: 2}, []model.GroundStation{station}, assets, windows)
	suggestions := generator.Generate(group)
	keep := keepSuggestion(t, suggestions)
	assertIDs(t, "keep", keep.KeepWindowIDs, []uint{11, 12})
	assertIDs(t, "move", keep.MoveWindowIDs, []uint{13})
	if keep.RequiresManual {
		t.Fatal("two-channel station must not escalate a three-window conflict to manual")
	}
	if keep.KeepCount != 2 || keep.MoveCount != 1 || keep.AvailableChannels != 2 {
		t.Fatalf("unexpected counts: keep=%d move=%d channels=%d", keep.KeepCount, keep.MoveCount, keep.AvailableChannels)
	}
	for _, suggestion := range suggestions {
		if suggestion.KeepCount != len(suggestion.KeepWindowIDs) || suggestion.MoveCount != len(suggestion.MoveWindowIDs) {
			t.Fatalf("%s exposes inconsistent counts", suggestion.ActionKey)
		}
		if suggestion.AvailableChannels != 2 {
			t.Fatalf("%s exposes %d channels, want 2", suggestion.ActionKey, suggestion.AvailableChannels)
		}
	}
}

func TestKeepHighestPlacesLockedWindowsFirst(t *testing.T) {
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	station := model.GroundStation{ID: 1, StationCode: "GS-2CH", AntennaCount: 2, SupportedBandsJSON: `["S"]`, StationStatus: "active"}
	asset := model.SatelliteAsset{ID: 1, SatelliteCode: "SAT-1", PriorityWeight: 0, SupportedBandsJSON: `["S"]`}
	windows := []model.ContactWindow{
		{ID: 21, StationID: 1, SatelliteID: 1, StartAt: base, EndAt: base.Add(10 * time.Minute), Band: "S", Priority: 5, Version: 1},
		{ID: 22, StationID: 1, SatelliteID: 1, StartAt: base.Add(time.Minute), EndAt: base.Add(9 * time.Minute), Band: "S", Priority: 4, Version: 1},
		{ID: 23, StationID: 1, SatelliteID: 1, StartAt: base.Add(2 * time.Minute), EndAt: base.Add(8 * time.Minute), Band: "S", Priority: 1, Locked: true, Version: 1},
	}
	group := newGroup(constants.ConflictTypeStationCapacity, windows, station.AntennaCount, 3, 0, "capacity", nil)
	generator := NewCandidateGenerator(Weights{PriorityLoss: 4, MovementDistance: .02, ContactDuration: .003, ResourceMargin: 2}, []model.GroundStation{station}, []model.SatelliteAsset{asset}, windows)
	keep := keepSuggestion(t, generator.Generate(group))
	assertIDs(t, "keep", keep.KeepWindowIDs, []uint{23, 21})
	assertIDs(t, "move", keep.MoveWindowIDs, []uint{22})
	if keep.RequiresManual {
		t.Fatal("locked window within channel capacity must not escalate to manual")
	}
}

func TestKeepHighestLockedOverflowKeepsAllAndRequiresManual(t *testing.T) {
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	station := model.GroundStation{ID: 1, StationCode: "GS-1CH", AntennaCount: 1, SupportedBandsJSON: `["S"]`, StationStatus: "active"}
	asset := model.SatelliteAsset{ID: 1, SatelliteCode: "SAT-1", PriorityWeight: 0, SupportedBandsJSON: `["S"]`}
	windows := []model.ContactWindow{
		{ID: 31, StationID: 1, SatelliteID: 1, StartAt: base, EndAt: base.Add(10 * time.Minute), Band: "S", Priority: 1, Locked: true, Version: 1},
		{ID: 32, StationID: 1, SatelliteID: 1, StartAt: base.Add(time.Minute), EndAt: base.Add(9 * time.Minute), Band: "S", Priority: 2, Locked: true, Version: 1},
		{ID: 33, StationID: 1, SatelliteID: 1, StartAt: base.Add(2 * time.Minute), EndAt: base.Add(8 * time.Minute), Band: "S", Priority: 9, Version: 1},
	}
	group := newGroup(constants.ConflictTypeStationCapacity, windows, station.AntennaCount, 3, 0, "capacity", nil)
	generator := NewCandidateGenerator(Weights{PriorityLoss: 4, MovementDistance: .02, ContactDuration: .003, ResourceMargin: 2}, []model.GroundStation{station}, []model.SatelliteAsset{asset}, windows)
	keep := keepSuggestion(t, generator.Generate(group))
	assertIDs(t, "keep", keep.KeepWindowIDs, []uint{32, 31})
	assertIDs(t, "move", keep.MoveWindowIDs, []uint{33})
	if !keep.RequiresManual {
		t.Fatal("locked windows exceeding channel capacity must escalate to manual")
	}
	if keep.KeepCount != 2 || keep.MoveCount != 1 || keep.AvailableChannels != 1 {
		t.Fatalf("unexpected counts: keep=%d move=%d channels=%d", keep.KeepCount, keep.MoveCount, keep.AvailableChannels)
	}
}
