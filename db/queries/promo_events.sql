-- name: InsertPromoEvent :execrows
INSERT INTO promo_events (
    org_id,
    source_id,
    store_id,
    sku,
    titulo,
    preco,
    preco_anterior,
    moeda,
    estoque_disponivel,
    badge_promo,
    url,
    detected_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
ON CONFLICT (org_id, source_id, preco, dedup_bucket) DO NOTHING;
