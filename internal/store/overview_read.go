package store

import (
	"context"
	"fmt"
	"time"
)

// MonitorStatusCounts is the derived monitor status aggregate used by the
// dashboard overview.
type MonitorStatusCounts struct {
	Total   int
	Active  int
	Paused  int
	Expired int
}

// DeliveryStatusCounts is the persisted delivery status aggregate. Total is the
// sum of the six status counters.
type DeliveryStatusCounts struct {
	Total          int
	Pending        int
	Sending        int
	Sent           int
	RetryScheduled int
	Failed         int
	Skipped        int
}

const monitorStatusCountsQuery = `SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN expired_at IS NOT NULL OR julianday(expires_at) <= julianday(?) THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN expired_at IS NULL AND julianday(expires_at) > julianday(?) AND enabled = 0 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN expired_at IS NULL AND julianday(expires_at) > julianday(?) AND enabled = 1 THEN 1 ELSE 0 END), 0)
FROM monitors`

const deliveryStatusCountsQuery = `SELECT status, COUNT(*) FROM notification_deliveries GROUP BY status`

// CountMonitorsByStatus returns bounded aggregate counts using the same
// expiry/paused rules as domain.Monitor.Status.
func (s *Store) CountMonitorsByStatus(ctx context.Context, now time.Time) (MonitorStatusCounts, error) {
	if now.IsZero() {
		return MonitorStatusCounts{}, fmt.Errorf("store: monitor count time is required")
	}
	db, err := s.executor()
	if err != nil {
		return MonitorStatusCounts{}, err
	}
	var counts MonitorStatusCounts
	when := formatTime(now)
	if err := db.QueryRowContext(ctx, monitorStatusCountsQuery, when, when, when).Scan(
		&counts.Total, &counts.Expired, &counts.Paused, &counts.Active,
	); err != nil {
		return MonitorStatusCounts{}, wrap("count monitors by status", err)
	}
	if counts.Total != counts.Active+counts.Paused+counts.Expired {
		return MonitorStatusCounts{}, fmt.Errorf("store: monitor status counts are inconsistent")
	}
	return counts, nil
}

// CountDeliveriesByStatus returns aggregate delivery status counts without
// loading delivery rows.
func (s *Store) CountDeliveriesByStatus(ctx context.Context) (DeliveryStatusCounts, error) {
	db, err := s.executor()
	if err != nil {
		return DeliveryStatusCounts{}, err
	}
	rows, err := db.QueryContext(ctx, deliveryStatusCountsQuery)
	if err != nil {
		return DeliveryStatusCounts{}, wrap("count deliveries by status", err)
	}
	defer rows.Close()

	var counts DeliveryStatusCounts
	for rows.Next() {
		var (
			status string
			count  int
		)
		if err := rows.Scan(&status, &count); err != nil {
			return DeliveryStatusCounts{}, wrap("scan delivery status count", err)
		}
		if count < 0 {
			return DeliveryStatusCounts{}, fmt.Errorf("store: delivery status count is negative")
		}
		switch DeliveryStatus(status) {
		case DeliveryPending:
			counts.Pending = count
		case DeliverySending:
			counts.Sending = count
		case DeliverySent:
			counts.Sent = count
		case DeliveryRetryScheduled:
			counts.RetryScheduled = count
		case DeliveryFailed:
			counts.Failed = count
		case DeliverySkipped:
			counts.Skipped = count
		default:
			return DeliveryStatusCounts{}, fmt.Errorf("store: invalid delivery status %q", status)
		}
		counts.Total += count
	}
	if err := rows.Err(); err != nil {
		return DeliveryStatusCounts{}, wrap("iterate delivery status counts", err)
	}
	if err := rows.Close(); err != nil {
		return DeliveryStatusCounts{}, wrap("close delivery status counts", err)
	}
	return counts, nil
}
