package inventory

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxTextLen     = 128
	maxNoteLen     = 500
	maxQuantity    = 1_000_000_000
	maxDailyWindow = 366
)

type itemKey struct {
	sku       string
	warehouse string
}

type Store struct {
	mu           sync.RWMutex
	now          func() time.Time
	items        map[itemKey]Item
	movements    []Movement
	activity     []Activity
	nextMovement int64
	nextActivity int64
}

func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{
		now:   now,
		items: make(map[itemKey]Item),
	}
}

func (s *Store) ListItems(warehouse string) []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		if warehouse != "" && it.Warehouse != warehouse {
			continue
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SKU != out[j].SKU {
			return out[i].SKU < out[j].SKU
		}
		return out[i].Warehouse < out[j].Warehouse
	})
	return out
}

func (s *Store) GetItem(sku, warehouse string) (Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	it, ok := s.items[itemKey{sku, warehouse}]
	if !ok {
		return Item{}, ErrNotFound
	}
	return it, nil
}

func (s *Store) CreateItem(in Item) (Item, error) {
	in.SKU = strings.TrimSpace(in.SKU)
	in.Name = strings.TrimSpace(in.Name)
	in.Category = strings.TrimSpace(in.Category)
	in.Warehouse = strings.TrimSpace(in.Warehouse)

	if err := validateItem(in); err != nil {
		return Item{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := itemKey{in.SKU, in.Warehouse}
	if _, exists := s.items[key]; exists {
		return Item{}, ErrConflict
	}
	s.items[key] = in
	return in, nil
}

func (s *Store) RecordMovement(in MovementInput) (Movement, error) {
	in.SKU = strings.TrimSpace(in.SKU)
	in.Warehouse = strings.TrimSpace(in.Warehouse)
	in.ToWarehouse = strings.TrimSpace(in.ToWarehouse)
	in.Note = strings.TrimSpace(in.Note)

	if err := validateMovement(in); err != nil {
		return Movement{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	srcKey := itemKey{in.SKU, in.Warehouse}
	src, ok := s.items[srcKey]
	if !ok {
		return Movement{}, ErrNotFound
	}

	switch in.Kind {
	case Received:
		src.Quantity += in.Quantity
	case Shipped, Transferred:
		if src.Quantity < in.Quantity {
			return Movement{}, ErrInsufficientStock
		}
		src.Quantity -= in.Quantity
	case Adjusted:
		if src.Quantity+in.Quantity < 0 {
			return Movement{}, ErrInsufficientStock
		}
		src.Quantity += in.Quantity
	}

	if in.Kind == Transferred {
		dstKey := itemKey{in.SKU, in.ToWarehouse}
		dst, ok := s.items[dstKey]
		if !ok {
			dst = src
			dst.Warehouse = in.ToWarehouse
			dst.Quantity = 0
		}
		dst.Quantity += in.Quantity
		s.items[dstKey] = dst
	}
	s.items[srcKey] = src

	now := s.now().UTC()
	s.nextMovement++
	m := Movement{
		ID:          s.nextMovement,
		Kind:        in.Kind,
		SKU:         in.SKU,
		Quantity:    in.Quantity,
		Warehouse:   in.Warehouse,
		ToWarehouse: in.ToWarehouse,
		Note:        in.Note,
		CreatedAt:   now,
	}
	s.movements = append(s.movements, m)

	s.nextActivity++
	s.activity = append(s.activity, Activity{
		ID:         s.nextActivity,
		Kind:       m.Kind,
		Text:       describe(m, src.Name),
		Time:       now,
		MovementID: m.ID,
	})

	return m, nil
}

func (s *Store) ListMovements(f MovementFilter) []Movement {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Movement, 0)
	for i := len(s.movements) - 1; i >= 0; i-- {
		m := s.movements[i]
		if f.SKU != "" && m.SKU != f.SKU {
			continue
		}
		if f.Warehouse != "" && m.Warehouse != f.Warehouse && m.ToWarehouse != f.Warehouse {
			continue
		}
		out = append(out, m)
		if f.Limit > 0 && len(out) == f.Limit {
			break
		}
	}
	return out
}

func (s *Store) DailyMovements(days int) ([]MovementDay, error) {
	if days < 1 || days > maxDailyWindow {
		return nil, &ValidationError{"days", fmt.Sprintf("must be between 1 and %d", maxDailyWindow)}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	today := s.now().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(days - 1))

	out := make([]MovementDay, days)
	index := make(map[string]int, days)
	for i := range out {
		d := start.AddDate(0, 0, i).Format(time.DateOnly)
		out[i].Date = d
		index[d] = i
	}

	for _, m := range s.movements {
		if m.CreatedAt.Before(start) {
			continue
		}
		i, ok := index[m.CreatedAt.Format(time.DateOnly)]
		if !ok {
			continue
		}
		switch {
		case m.Kind == Received, m.Kind == Adjusted && m.Quantity > 0:
			out[i].Inbound += m.Quantity
		case m.Kind == Shipped:
			out[i].Outbound += m.Quantity
		case m.Kind == Adjusted && m.Quantity < 0:
			out[i].Outbound -= m.Quantity
		}
	}
	return out, nil
}

func (s *Store) ListActivity(limit int) []Activity {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := len(s.activity)
	if limit > 0 && limit < n {
		n = limit
	}
	out := make([]Activity, 0, n)
	for i := len(s.activity) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, s.activity[i])
	}
	return out
}

func validateItem(it Item) error {
	if err := requireText("sku", it.SKU); err != nil {
		return err
	}
	if err := requireText("name", it.Name); err != nil {
		return err
	}
	if err := requireText("warehouse", it.Warehouse); err != nil {
		return err
	}
	if len(it.Category) > maxTextLen {
		return &ValidationError{"category", fmt.Sprintf("must be at most %d characters", maxTextLen)}
	}
	if it.Quantity < 0 || it.Quantity > maxQuantity {
		return &ValidationError{"quantity", fmt.Sprintf("must be between 0 and %d", maxQuantity)}
	}
	if it.ReorderPoint < 0 || it.ReorderPoint > maxQuantity {
		return &ValidationError{"reorderPoint", fmt.Sprintf("must be between 0 and %d", maxQuantity)}
	}
	if it.UnitCost < 0 || math.IsNaN(it.UnitCost) || math.IsInf(it.UnitCost, 0) {
		return &ValidationError{"unitCost", "must be a non-negative number"}
	}
	return nil
}

func validateMovement(in MovementInput) error {
	if !in.Kind.Valid() {
		return &ValidationError{"kind", "must be one of received, shipped, adjusted, transferred"}
	}
	if err := requireText("sku", in.SKU); err != nil {
		return err
	}
	if err := requireText("warehouse", in.Warehouse); err != nil {
		return err
	}
	if len(in.Note) > maxNoteLen {
		return &ValidationError{"note", fmt.Sprintf("must be at most %d characters", maxNoteLen)}
	}

	if in.Kind == Adjusted {
		if in.Quantity == 0 || in.Quantity < -maxQuantity || in.Quantity > maxQuantity {
			return &ValidationError{"quantity", fmt.Sprintf("must be non-zero and between -%d and %d", maxQuantity, maxQuantity)}
		}
	} else if in.Quantity <= 0 || in.Quantity > maxQuantity {
		return &ValidationError{"quantity", fmt.Sprintf("must be between 1 and %d", maxQuantity)}
	}

	if in.Kind == Transferred {
		if err := requireText("toWarehouse", in.ToWarehouse); err != nil {
			return err
		}
		if in.ToWarehouse == in.Warehouse {
			return &ValidationError{"toWarehouse", "must differ from warehouse"}
		}
	} else if in.ToWarehouse != "" {
		return &ValidationError{"toWarehouse", "only allowed for transferred movements"}
	}
	return nil
}

func requireText(field, v string) error {
	if v == "" {
		return &ValidationError{field, "is required"}
	}
	if len(v) > maxTextLen {
		return &ValidationError{field, fmt.Sprintf("must be at most %d characters", maxTextLen)}
	}
	return nil
}

func describe(m Movement, name string) string {
	label := m.SKU
	if name != "" {
		label = fmt.Sprintf("%s (%s)", m.SKU, name)
	}
	switch m.Kind {
	case Received:
		return fmt.Sprintf("Received %d × %s at %s", m.Quantity, label, m.Warehouse)
	case Shipped:
		return fmt.Sprintf("Shipped %d × %s from %s", m.Quantity, label, m.Warehouse)
	case Adjusted:
		return fmt.Sprintf("Adjusted %s at %s by %+d", label, m.Warehouse, m.Quantity)
	case Transferred:
		return fmt.Sprintf("Transferred %d × %s from %s to %s", m.Quantity, label, m.Warehouse, m.ToWarehouse)
	}
	return label
}
