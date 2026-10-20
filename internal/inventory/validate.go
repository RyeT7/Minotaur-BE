package inventory

import (
	"fmt"
	"math"
	"strings"
)

const (
	maxTextLen     = 128
	maxNoteLen     = 500
	maxQuantity    = 1_000_000_000
	maxUnitCost    = 9_999_999_999.99
	maxDailyWindow = 366
	maxListLimit   = 200
)

func normalizeItem(in Item) Item {
	in.SKU = strings.TrimSpace(in.SKU)
	in.Name = strings.TrimSpace(in.Name)
	in.Category = strings.TrimSpace(in.Category)
	in.Warehouse = strings.TrimSpace(in.Warehouse)
	return in
}

func normalizeMovement(in MovementInput) MovementInput {
	in.SKU = strings.TrimSpace(in.SKU)
	in.Warehouse = strings.TrimSpace(in.Warehouse)
	in.ToWarehouse = strings.TrimSpace(in.ToWarehouse)
	in.Note = strings.TrimSpace(in.Note)
	return in
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
	if it.UnitCost < 0 || it.UnitCost > maxUnitCost || math.IsNaN(it.UnitCost) {
		return &ValidationError{"unitCost", "must be a non-negative number below 10000000000"}
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
