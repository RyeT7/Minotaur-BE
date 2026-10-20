CREATE TABLE items (
    sku           text          NOT NULL CHECK (char_length(sku) BETWEEN 1 AND 128),
    warehouse     text          NOT NULL CHECK (char_length(warehouse) BETWEEN 1 AND 128),
    name          text          NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    category      text          NOT NULL DEFAULT '' CHECK (char_length(category) <= 128),
    quantity      bigint        NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    reorder_point bigint        NOT NULL DEFAULT 0 CHECK (reorder_point >= 0),
    unit_cost     numeric(12,2) NOT NULL DEFAULT 0 CHECK (unit_cost >= 0),
    created_at    timestamptz   NOT NULL DEFAULT now(),
    PRIMARY KEY (sku, warehouse)
);

CREATE INDEX items_warehouse_idx ON items (warehouse);

CREATE TABLE movements (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind         text        NOT NULL CHECK (kind IN ('received', 'shipped', 'adjusted', 'transferred')),
    sku          text        NOT NULL,
    warehouse    text        NOT NULL,
    to_warehouse text,
    quantity     bigint      NOT NULL,
    note         text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    created_at   timestamptz NOT NULL,
    FOREIGN KEY (sku, warehouse) REFERENCES items (sku, warehouse),
    FOREIGN KEY (sku, to_warehouse) REFERENCES items (sku, warehouse),
    CHECK ((kind = 'transferred') = (to_warehouse IS NOT NULL)),
    CHECK (to_warehouse IS DISTINCT FROM warehouse),
    CHECK (CASE WHEN kind = 'adjusted' THEN quantity <> 0 ELSE quantity > 0 END)
);

CREATE INDEX movements_created_at_idx ON movements (created_at);
CREATE INDEX movements_sku_idx ON movements (sku, id DESC);
CREATE INDEX movements_warehouse_idx ON movements (warehouse, id DESC);
CREATE INDEX movements_to_warehouse_idx ON movements (to_warehouse, id DESC) WHERE to_warehouse IS NOT NULL;

CREATE TABLE activity (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        text        NOT NULL CHECK (kind IN ('received', 'shipped', 'adjusted', 'transferred')),
    text        text        NOT NULL,
    movement_id bigint      NOT NULL REFERENCES movements (id),
    created_at  timestamptz NOT NULL
);
