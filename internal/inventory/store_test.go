package inventory

import (
	"errors"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s := NewStore(func() time.Time { return now })
	if _, err := s.CreateItem(Item{SKU: "WID-1", Name: "Widget", Warehouse: "WH-A", Quantity: 10, UnitCost: 2.5}); err != nil {
		t.Fatal(err)
	}
	return s, &now
}

func TestCreateItemConflict(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.CreateItem(Item{SKU: "WID-1", Name: "Widget", Warehouse: "WH-A"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
}

func TestRecordMovementUpdatesStockAndActivity(t *testing.T) {
	s, _ := newTestStore(t)

	steps := []struct {
		in   MovementInput
		want int
	}{
		{MovementInput{Kind: Received, SKU: "WID-1", Warehouse: "WH-A", Quantity: 5}, 15},
		{MovementInput{Kind: Shipped, SKU: "WID-1", Warehouse: "WH-A", Quantity: 3}, 12},
		{MovementInput{Kind: Adjusted, SKU: "WID-1", Warehouse: "WH-A", Quantity: -2}, 10},
	}
	for _, st := range steps {
		if _, err := s.RecordMovement(st.in); err != nil {
			t.Fatalf("%s: %v", st.in.Kind, err)
		}
		it, _ := s.GetItem("WID-1", "WH-A")
		if it.Quantity != st.want {
			t.Fatalf("%s: quantity %d, want %d", st.in.Kind, it.Quantity, st.want)
		}
	}

	act := s.ListActivity(0)
	if len(act) != 3 || act[0].Kind != Adjusted || act[0].Text != "Adjusted WID-1 (Widget) at WH-A by -2" {
		t.Fatalf("unexpected activity: %+v", act)
	}
}

func TestTransferCreatesDestination(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.RecordMovement(MovementInput{Kind: Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-B", Quantity: 4}); err != nil {
		t.Fatal(err)
	}
	src, _ := s.GetItem("WID-1", "WH-A")
	dst, err := s.GetItem("WID-1", "WH-B")
	if err != nil {
		t.Fatal(err)
	}
	if src.Quantity != 6 || dst.Quantity != 4 || dst.Name != "Widget" {
		t.Fatalf("src=%+v dst=%+v", src, dst)
	}
}

func TestRecordMovementErrors(t *testing.T) {
	s, _ := newTestStore(t)
	var ve *ValidationError

	cases := []struct {
		name  string
		in    MovementInput
		check func(error) bool
	}{
		{"insufficient", MovementInput{Kind: Shipped, SKU: "WID-1", Warehouse: "WH-A", Quantity: 11}, func(e error) bool { return errors.Is(e, ErrInsufficientStock) }},
		{"unknown item", MovementInput{Kind: Received, SKU: "NOPE", Warehouse: "WH-A", Quantity: 1}, func(e error) bool { return errors.Is(e, ErrNotFound) }},
		{"bad kind", MovementInput{Kind: "stolen", SKU: "WID-1", Warehouse: "WH-A", Quantity: 1}, func(e error) bool { return errors.As(e, &ve) && ve.Field == "kind" }},
		{"zero qty", MovementInput{Kind: Received, SKU: "WID-1", Warehouse: "WH-A"}, func(e error) bool { return errors.As(e, &ve) && ve.Field == "quantity" }},
		{"same warehouse", MovementInput{Kind: Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-A", Quantity: 1}, func(e error) bool { return errors.As(e, &ve) && ve.Field == "toWarehouse" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.RecordMovement(tc.in); !tc.check(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	it, _ := s.GetItem("WID-1", "WH-A")
	if it.Quantity != 10 || len(s.ListActivity(0)) != 0 {
		t.Fatalf("failed movements must not change state: %+v", it)
	}
}

func TestDailyMovements(t *testing.T) {
	s, now := newTestStore(t)

	*now = now.AddDate(0, 0, -1)
	mustMove(t, s, MovementInput{Kind: Received, SKU: "WID-1", Warehouse: "WH-A", Quantity: 7})
	*now = now.AddDate(0, 0, 1)
	mustMove(t, s, MovementInput{Kind: Shipped, SKU: "WID-1", Warehouse: "WH-A", Quantity: 3})
	mustMove(t, s, MovementInput{Kind: Adjusted, SKU: "WID-1", Warehouse: "WH-A", Quantity: -1})
	mustMove(t, s, MovementInput{Kind: Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-B", Quantity: 2})

	got, err := s.DailyMovements(3)
	if err != nil {
		t.Fatal(err)
	}
	want := []MovementDay{
		{Date: "2026-10-17"},
		{Date: "2026-10-18", Inbound: 7},
		{Date: "2026-10-19", Outbound: 4},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("day %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func mustMove(t *testing.T, s *Store, in MovementInput) {
	t.Helper()
	if _, err := s.RecordMovement(in); err != nil {
		t.Fatal(err)
	}
}
