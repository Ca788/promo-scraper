-- name: GetEligibleSources :many
SELECT
    id,
    org_id,
    store_id,
    url,
    strategy,
    interval_seconds,
    selectors,
    enabled,
    last_collected_at,
    last_snapshot,
    last_error,
    version,
    created_at,
    updated_at
FROM sources
WHERE org_id = $1
  AND enabled = true
  AND (
        last_collected_at IS NULL
     OR now() >= last_collected_at + (interval_seconds || ' seconds')::interval
  )
ORDER BY last_collected_at NULLS FIRST, id
LIMIT $2;

-- name: GetSourceByID :one
SELECT
    id,
    org_id,
    store_id,
    url,
    strategy,
    interval_seconds,
    selectors,
    enabled,
    last_collected_at,
    last_snapshot,
    last_error,
    version,
    created_at,
    updated_at
FROM sources
WHERE id = $1 AND org_id = $2;

-- name: UpdateSourceAfterCollect :execrows
UPDATE sources
SET
    last_snapshot     = $1,
    last_collected_at = now(),
    last_error        = NULL,
    version           = version + 1,
    updated_at        = now()
WHERE id = $2
  AND org_id = $3
  AND version = $4;

-- name: MarkSourceError :exec
UPDATE sources
SET
    last_error        = $1,
    last_collected_at = now(),
    version           = version + 1,
    updated_at        = now()
WHERE id = $2 AND org_id = $3;
