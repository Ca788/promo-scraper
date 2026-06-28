CREATE TABLE stores (
    id         bigserial PRIMARY KEY,
    org_id     uuid        NOT NULL,
    nome       text        NOT NULL,
    host       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, host)
);

CREATE TABLE sources (
    id                bigserial PRIMARY KEY,
    org_id            uuid        NOT NULL,
    store_id          bigint      NOT NULL REFERENCES stores (id),
    url               text        NOT NULL,
    strategy          text        NOT NULL CHECK (strategy IN ('http', 'headless')),
    interval_seconds  integer     NOT NULL,
    selectors         jsonb       NOT NULL,
    enabled           boolean     NOT NULL DEFAULT true,
    last_collected_at timestamptz,
    last_snapshot     jsonb,
    last_error        text,
    version           integer     NOT NULL DEFAULT 1,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, url)
);

CREATE INDEX sources_scheduler_idx ON sources (enabled, last_collected_at);
