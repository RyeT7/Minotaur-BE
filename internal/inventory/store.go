package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pgUniqueViolation = "23505"

type Store struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewStore(db *pgxpool.Pool, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

const itemColumns = "sku, name, category, warehouse, quantity, reorder_point, unit_cost"

func scanItem(row pgx.CollectableRow) (Item, error) {
	var it Item
	err := row.Scan(&it.SKU, &it.Name, &it.Category, &it.Warehouse, &it.Quantity, &it.ReorderPoint, &it.UnitCost)
	return it, err
}

func (s *Store) ListItems(ctx context.Context, warehouse string) ([]Item, error) {
	rows, err := s.db.Query(ctx,
		"SELECT "+itemColumns+" FROM items WHERE $1 = '' OR warehouse = $1 ORDER BY sku, warehouse",
		warehouse)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

func (s *Store) GetItem(ctx context.Context, sku, warehouse string) (Item, error) {
	rows, err := s.db.Query(ctx,
		"SELECT "+itemColumns+" FROM items WHERE sku = $1 AND warehouse = $2",
		sku, warehouse)
	if err != nil {
		return Item{}, err
	}
	it, err := pgx.CollectExactlyOneRow(rows, scanItem)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return it, err
}

func (s *Store) CreateItem(ctx context.Context, in Item) (Item, error) {
	in = normalizeItem(in)
	if err := validateItem(in); err != nil {
		return Item{}, err
	}

	rows, err := s.db.Query(ctx,
		`INSERT INTO items (sku, name, category, warehouse, quantity, reorder_point, unit_cost, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+itemColumns,
		in.SKU, in.Name, in.Category, in.Warehouse, in.Quantity, in.ReorderPoint, in.UnitCost, s.now().UTC())
	if err != nil {
		return Item{}, err
	}
	it, err := pgx.CollectExactlyOneRow(rows, scanItem)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return Item{}, ErrConflict
	}
	return it, err
}

func (s *Store) RecordMovement(ctx context.Context, in MovementInput) (Movement, error) {
	in = normalizeMovement(in)
	if err := validateMovement(in); err != nil {
		return Movement{}, err
	}

	var m Movement
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if in.Kind == Transferred {
			if _, err := tx.Exec(ctx,
				`INSERT INTO items (sku, name, category, warehouse, quantity, reorder_point, unit_cost, created_at)
				SELECT sku, name, category, $3, 0, reorder_point, unit_cost, $4
				FROM items WHERE sku = $1 AND warehouse = $2
				ON CONFLICT (sku, warehouse) DO NOTHING`,
				in.SKU, in.Warehouse, in.ToWarehouse, s.now().UTC()); err != nil {
				return err
			}
		}

		rows, err := tx.Query(ctx,
			`SELECT warehouse, name, quantity FROM items
			WHERE sku = $1 AND warehouse IN ($2, $3)
			ORDER BY warehouse
			FOR UPDATE`,
			in.SKU, in.Warehouse, in.ToWarehouse)
		if err != nil {
			return err
		}
		type locked struct {
			warehouse string
			name      string
			quantity  int
		}
		lockedRows, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (locked, error) {
			var l locked
			err := r.Scan(&l.warehouse, &l.name, &l.quantity)
			return l, err
		})
		if err != nil {
			return err
		}

		var src *locked
		for i := range lockedRows {
			if lockedRows[i].warehouse == in.Warehouse {
				src = &lockedRows[i]
			}
		}
		if src == nil {
			return ErrNotFound
		}

		delta := in.Quantity
		switch in.Kind {
		case Shipped, Transferred:
			delta = -in.Quantity
		}
		if src.quantity+delta < 0 {
			return ErrInsufficientStock
		}

		if _, err := tx.Exec(ctx,
			"UPDATE items SET quantity = quantity + $3 WHERE sku = $1 AND warehouse = $2",
			in.SKU, in.Warehouse, delta); err != nil {
			return err
		}
		if in.Kind == Transferred {
			if _, err := tx.Exec(ctx,
				"UPDATE items SET quantity = quantity + $3 WHERE sku = $1 AND warehouse = $2",
				in.SKU, in.ToWarehouse, in.Quantity); err != nil {
				return err
			}
		}

		now := s.now().UTC()
		var toWarehouse *string
		if in.ToWarehouse != "" {
			toWarehouse = &in.ToWarehouse
		}
		m = Movement{
			Kind:        in.Kind,
			SKU:         in.SKU,
			Quantity:    in.Quantity,
			Warehouse:   in.Warehouse,
			ToWarehouse: in.ToWarehouse,
			Note:        in.Note,
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO movements (kind, sku, warehouse, to_warehouse, quantity, note, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, created_at`,
			in.Kind, in.SKU, in.Warehouse, toWarehouse, in.Quantity, in.Note, now).Scan(&m.ID, &m.CreatedAt); err != nil {
			return err
		}
		m.CreatedAt = m.CreatedAt.UTC()

		_, err = tx.Exec(ctx,
			"INSERT INTO activity (kind, text, movement_id, created_at) VALUES ($1, $2, $3, $4)",
			m.Kind, describe(m, src.name), m.ID, now)
		return err
	})
	if err != nil {
		return Movement{}, err
	}
	return m, nil
}

func (s *Store) ListMovements(ctx context.Context, f MovementFilter) ([]Movement, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = maxListLimit
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, kind, sku, quantity, warehouse, COALESCE(to_warehouse, ''), note, created_at
		FROM movements
		WHERE ($1 = '' OR sku = $1)
		  AND ($2 = '' OR warehouse = $2 OR to_warehouse = $2)
		ORDER BY id DESC
		LIMIT $3`,
		f.SKU, f.Warehouse, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Movement, error) {
		var m Movement
		err := r.Scan(&m.ID, &m.Kind, &m.SKU, &m.Quantity, &m.Warehouse, &m.ToWarehouse, &m.Note, &m.CreatedAt)
		m.CreatedAt = m.CreatedAt.UTC()
		return m, err
	})
}

func (s *Store) DailyMovements(ctx context.Context, days int) ([]MovementDay, error) {
	if days < 1 || days > maxDailyWindow {
		return nil, &ValidationError{"days", fmt.Sprintf("must be between 1 and %d", maxDailyWindow)}
	}

	today := s.now().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(days - 1))

	rows, err := s.db.Query(ctx,
		`SELECT to_char(g.day, 'YYYY-MM-DD'),
			COALESCE(SUM(CASE
				WHEN m.kind = 'received' THEN m.quantity
				WHEN m.kind = 'adjusted' AND m.quantity > 0 THEN m.quantity
			END), 0)::bigint,
			COALESCE(SUM(CASE
				WHEN m.kind = 'shipped' THEN m.quantity
				WHEN m.kind = 'adjusted' AND m.quantity < 0 THEN -m.quantity
			END), 0)::bigint
		FROM generate_series($1::timestamp, $2::timestamp, interval '1 day') AS g(day)
		LEFT JOIN movements m
			ON m.created_at >= (g.day AT TIME ZONE 'UTC')
			AND m.created_at < ((g.day + interval '1 day') AT TIME ZONE 'UTC')
		GROUP BY g.day
		ORDER BY g.day`,
		start.Format(time.DateOnly), today.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MovementDay, error) {
		var d MovementDay
		err := r.Scan(&d.Date, &d.Inbound, &d.Outbound)
		return d, err
	})
}

func (s *Store) ListActivity(ctx context.Context, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = maxListLimit
	}
	rows, err := s.db.Query(ctx,
		"SELECT id, kind, text, created_at, movement_id FROM activity ORDER BY id DESC LIMIT $1",
		limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Activity, error) {
		var a Activity
		err := r.Scan(&a.ID, &a.Kind, &a.Text, &a.Time, &a.MovementID)
		a.Time = a.Time.UTC()
		return a, err
	})
}
