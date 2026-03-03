package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TelemetryRow is a single time-series data point from the telemetry hypertable.
type TelemetryRow struct {
	Time     time.Time `json:"time"`
	DeviceID uuid.UUID `json:"device_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	Metric   string    `json:"metric"`
	Value    float64   `json:"value"`
	Tags     []byte    `json:"tags,omitempty"` // JSONB, may be null
}

// InsertTelemetryBatch inserts multiple telemetry rows in a single DB round-trip.
// Uses pgx SendBatch for efficiency with large ingest payloads.
func InsertTelemetryBatch(ctx context.Context, pool *pgxpool.Pool, rows []TelemetryRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(
			`INSERT INTO telemetry (time, device_id, tenant_id, metric, value, tags)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT DO NOTHING`,
			r.Time, r.DeviceID, r.TenantID, r.Metric, r.Value, r.Tags,
		)
	}
	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range rows {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("telemetry batch exec: %w", err)
		}
	}
	return nil
}

// QueryTelemetry returns time-ordered rows for a device+metric within [from, to].
// limit is capped at 10 000; defaults to 1 000 when ≤ 0.
func QueryTelemetry(
	ctx context.Context,
	pool *pgxpool.Pool,
	deviceID uuid.UUID,
	metric string,
	from, to time.Time,
	limit int,
) ([]TelemetryRow, error) {
	if limit <= 0 || limit > 10_000 {
		limit = 1_000
	}
	rows, err := pool.Query(ctx,
		`SELECT time, device_id, tenant_id, metric, value, tags
		 FROM telemetry
		 WHERE device_id = $1
		   AND metric    = $2
		   AND time BETWEEN $3 AND $4
		 ORDER BY time DESC
		 LIMIT $5`,
		deviceID, metric, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query telemetry: %w", err)
	}
	defer rows.Close()

	var result []TelemetryRow
	for rows.Next() {
		var r TelemetryRow
		if err := rows.Scan(&r.Time, &r.DeviceID, &r.TenantID, &r.Metric, &r.Value, &r.Tags); err != nil {
			return nil, fmt.Errorf("scan telemetry: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
