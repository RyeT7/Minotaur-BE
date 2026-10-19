package inventory

import (
	"errors"
	"time"
)

type MovementKind string

const (
	Received    MovementKind = "received"
	Shipped     MovementKind = "shipped"
	Adjusted    MovementKind = "adjusted"
	Transferred MovementKind = "transferred"
)

func (k MovementKind) Valid() bool {
	switch k {
	case Received, Shipped, Adjusted, Transferred:
		return true
	}
	return false
}

type Item struct {
	SKU          string  `json:"sku"`
	Name         string  `json:"name"`
	Category     string  `json:"category"`
	Warehouse    string  `json:"warehouse"`
	Quantity     int     `json:"quantity"`
	ReorderPoint int     `json:"reorderPoint"`
	UnitCost     float64 `json:"unitCost"`
}

type Movement struct {
	ID          int64        `json:"id"`
	Kind        MovementKind `json:"kind"`
	SKU         string       `json:"sku"`
	Quantity    int          `json:"quantity"`
	Warehouse   string       `json:"warehouse"`
	ToWarehouse string       `json:"toWarehouse,omitempty"`
	Note        string       `json:"note,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
}

type MovementInput struct {
	Kind        MovementKind `json:"kind"`
	SKU         string       `json:"sku"`
	Quantity    int          `json:"quantity"`
	Warehouse   string       `json:"warehouse"`
	ToWarehouse string       `json:"toWarehouse"`
	Note        string       `json:"note"`
}

type MovementFilter struct {
	SKU       string
	Warehouse string
	Limit     int
}

type MovementDay struct {
	Date     string `json:"date"`
	Inbound  int    `json:"inbound"`
	Outbound int    `json:"outbound"`
}

type Activity struct {
	ID         int64        `json:"id"`
	Kind       MovementKind `json:"kind"`
	Text       string       `json:"text"`
	Time       time.Time    `json:"time"`
	MovementID int64        `json:"movementId"`
}

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("already exists")
	ErrInsufficientStock = errors.New("insufficient stock")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
