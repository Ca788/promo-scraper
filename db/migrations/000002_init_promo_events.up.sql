CREATE TABLE promo_events (
    id                  bigserial    PRIMARY KEY,
    org_id              uuid         NOT NULL,
    source_id           bigint       NOT NULL REFERENCES sources (id),
    store_id            bigint       NOT NULL REFERENCES stores (id),
    sku                 text         NOT NULL,
    titulo              text         NOT NULL,
    preco               numeric(12,2) NOT NULL,
    preco_anterior      numeric(12,2),
    moeda               text         NOT NULL DEFAULT 'BRL',
    estoque_disponivel  boolean      NOT NULL,
    badge_promo         boolean      NOT NULL,
    url                 text         NOT NULL,
    detected_at         timestamptz  NOT NULL DEFAULT now(),
    dedup_bucket        timestamptz  GENERATED ALWAYS AS (
        (
            date_trunc('hour', (detected_at AT TIME ZONE 'UTC'))
            + interval '30 min'
              * (extract(minute from (detected_at AT TIME ZONE 'UTC'))::int / 30)
        ) AT TIME ZONE 'UTC'
    ) STORED,
    UNIQUE (org_id, source_id, preco, dedup_bucket)
);

CREATE INDEX promo_events_org_detected_idx
    ON promo_events (org_id, detected_at DESC);

CREATE INDEX promo_events_org_source_detected_idx
    ON promo_events (org_id, source_id, detected_at DESC);
