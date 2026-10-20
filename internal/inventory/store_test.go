package inventory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/RyeT7/Minotaur-BE/internal/inventory"
	"github.com/RyeT7/Minotaur-BE/internal/testdb"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestStore(t *testing.T) (*inventory.Store, *clock) {
	t.Helper()
	clk := &clock{t: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	s := inventory.NewStore(testdb.New(t), clk.now)
	if _, err := s.CreateItem(context.Background(), inventory.Item{SKU: "WID-1", Name: "Widget", Warehouse: "WH-A", Quantity: 10, UnitCost: 2.5}); err != nil {
		t.Fatal(err)
	}
	return s, clk
}

func mustMove(t *testing.T, s *inventory.Store, in inventory.MovementInput) {
	t.Helper()
	if _, err := s.RecordMovement(context.Background(), in); err != nil {
		t.Fatal(err)
	}
}

func mustItem(t *testing.T, s *inventory.Store, sku, warehouse string) inventory.Item {
	t.Helper()
	it, err := s.GetItem(context.Background(), sku, warehouse)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestCreateItem(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	it := mustItem(t, s, "WID-1", "WH-A")
	if it.UnitCost != 2.5 || it.Quantity != 10 {
		t.Fatalf("unexpected item: %+v", it)
	}

	if _, err := s.CreateItem(ctx, inventory.Item{SKU: "WID-1", Name: "Widget", Warehouse: "WH-A"}); !errors.Is(err, inventory.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}

	items, err := s.ListItems(ctx, "WH-B")
	if err != nil || len(items) != 0 {
		t.Fatalf("filter by warehouse: %v %+v", err, items)
	}
}

func TestRecordMovementUpdatesStockAndActivity(t *testing.T) {
	s, _ := newTestStore(t)

	steps := []struct {
		in   inventory.MovementInput
		want int
	}{
		{inventory.MovementInput{Kind: inventory.Received, SKU: "WID-1", Warehouse: "WH-A", Quantity: 5}, 15},
		{inventory.MovementInput{Kind: inventory.Shipped, SKU: "WID-1", Warehouse: "WH-A", Quantity: 3}, 12},
		{inventory.MovementInput{Kind: inventory.Adjusted, SKU: "WID-1", Warehouse: "WH-A", Quantity: -2}, 10},
	}
	for _, st := range steps {
		mustMove(t, s, st.in)
		if got := mustItem(t, s, "WID-1", "WH-A").Quantity; got != st.want {
			t.Fatalf("%s: quantity %d, want %d", st.in.Kind, got, st.want)
		}
	}

	act, err := s.ListActivity(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(act) != 3 || act[0].Kind != inventory.Adjusted || act[0].Text != "Adjusted WID-1 (Widget) at WH-A by -2" {
		t.Fatalf("unexpected activity: %+v", act)
	}

	movements, err := s.ListMovements(context.Background(), inventory.MovementFilter{SKU: "WID-1", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(movements) != 2 || movements[0].Kind != inventory.Adjusted || movements[1].Kind != inventory.Shipped {
		t.Fatalf("unexpected movements: %+v", movements)
	}
}

func TestTransferCreatesDestination(t *testing.T) {
	s, _ := newTestStore(t)
	mustMove(t, s, inventory.MovementInput{Kind: inventory.Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-B", Quantity: 4})

	src := mustItem(t, s, "WID-1", "WH-A")
	dst := mustItem(t, s, "WID-1", "WH-B")
	if src.Quantity != 6 || dst.Quantity != 4 || dst.Name != "Widget" || dst.UnitCost != 2.5 {
		t.Fatalf("src=%+v dst=%+v", src, dst)
	}

	movements, err := s.ListMovements(context.Background(), inventory.MovementFilter{Warehouse: "WH-B"})
	if err != nil || len(movements) != 1 || movements[0].ToWarehouse != "WH-B" {
		t.Fatalf("filter by destination: %v %+v", err, movements)
	}
}

func TestRecordMovementErrors(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	var ve *inventory.ValidationError

	cases := []struct {
		name  string
		in    inventory.MovementInput
		check func(error) bool
	}{
		{"insufficient", inventory.MovementInput{Kind: inventory.Shipped, SKU: "WID-1", Warehouse: "WH-A", Quantity: 11}, func(e error) bool { return errors.Is(e, inventory.ErrInsufficientStock) }},
		{"insufficient transfer", inventory.MovementInput{Kind: inventory.Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-B", Quantity: 11}, func(e error) bool { return errors.Is(e, inventory.ErrInsufficientStock) }},
		{"unknown item", inventory.MovementInput{Kind: inventory.Received, SKU: "NOPE", Warehouse: "WH-A", Quantity: 1}, func(e error) bool { return errors.Is(e, inventory.ErrNotFound) }},
		{"unknown transfer source", inventory.MovementInput{Kind: inventory.Transferred, SKU: "NOPE", Warehouse: "WH-A", ToWarehouse: "WH-B", Quantity: 1}, func(e error) bool { return errors.Is(e, inventory.ErrNotFound) }},
		{"bad kind", inventory.MovementInput{Kind: "stolen", SKU: "WID-1", Warehouse: "WH-A", Quantity: 1}, func(e error) bool { return errors.As(e, &ve) && ve.Field == "kind" }},
		{"zero qty", inventory.MovementInput{Kind: inventory.Received, SKU: "WID-1", Warehouse: "WH-A"}, func(e error) bool { return errors.As(e, &ve) && ve.Field == "quantity" }},
		{"same warehouse", inventory.MovementInput{Kind: inventory.Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-A", Quantity: 1}, func(e error) bool { return errors.As(e, &ve) && ve.Field == "toWarehouse" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.RecordMovement(ctx, tc.in); !tc.check(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	if it := mustItem(t, s, "WID-1", "WH-A"); it.Quantity != 10 {
		t.Fatalf("failed movements must not change stock: %+v", it)
	}
	if _, err := s.GetItem(ctx, "WID-1", "WH-B"); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("failed transfer must not create destination: %v", err)
	}
	if act, _ := s.ListActivity(ctx, 0); len(act) != 0 {
		t.Fatalf("failed movements must not log activity: %+v", act)
	}
}

func TestConcurrentShipmentsNeverOversell(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	shipped := 0
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.RecordMovement(ctx, inventory.MovementInput{Kind: inventory.Shipped, SKU: "WID-1", Warehouse: "WH-A", Quantity: 1})
			if err == nil {
				mu.Lock()
				shipped++
				mu.Unlock()
			} else if !errors.Is(err, inventory.ErrInsufficientStock) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if it := mustItem(t, s, "WID-1", "WH-A"); shipped != 10 || it.Quantity != 0 {
		t.Fatalf("shipped %d, remaining %d; want 10 and 0", shipped, it.Quantity)
	}
}

func TestConcurrentOpposingTransfers(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	mustMove(t, s, inventory.MovementInput{Kind: inventory.Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-B", Quantity: 5})

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			from, to := "WH-A", "WH-B"
			if i%2 == 1 {
				from, to = to, from
			}
			if _, err := s.RecordMovement(ctx, inventory.MovementInput{Kind: inventory.Transferred, SKU: "WID-1", Warehouse: from, ToWarehouse: to, Quantity: 1}); err != nil && !errors.Is(err, inventory.ErrInsufficientStock) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	a, b := mustItem(t, s, "WID-1", "WH-A"), mustItem(t, s, "WID-1", "WH-B")
	if a.Quantity+b.Quantity != 10 {
		t.Fatalf("stock not conserved: A=%d B=%d", a.Quantity, b.Quantity)
	}
}

func TestDailyMovements(t *testing.T) {
	s, clk := newTestStore(t)

	clk.add(-24 * time.Hour)
	mustMove(t, s, inventory.MovementInput{Kind: inventory.Received, SKU: "WID-1", Warehouse: "WH-A", Quantity: 7})
	clk.add(24 * time.Hour)
	mustMove(t, s, inventory.MovementInput{Kind: inventory.Shipped, SKU: "WID-1", Warehouse: "WH-A", Quantity: 3})
	mustMove(t, s, inventory.MovementInput{Kind: inventory.Adjusted, SKU: "WID-1", Warehouse: "WH-A", Quantity: -1})
	mustMove(t, s, inventory.MovementInput{Kind: inventory.Transferred, SKU: "WID-1", Warehouse: "WH-A", ToWarehouse: "WH-B", Quantity: 2})

	got, err := s.DailyMovements(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []inventory.MovementDay{
		{Date: "2026-10-17"},
		{Date: "2026-10-18", Inbound: 7},
		{Date: "2026-10-19", Outbound: 4},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("day %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
