package store

import (
	"context"
	"fmt"
	"time"
)

// Download is one logged enclosure fetch (BlogCFC's download.cfm and its
// downloadtracker): who took it, from where, and whether they played it
// online rather than downloading it.
type Download struct {
	ID           string
	EntryID      string
	IP           string
	Referrer     string
	UserAgent    string
	Enclosure    string
	DownloadedAt time.Time
	Online       bool
}

// LogDownload records a fetch, filling ID and DownloadedAt when they are
// zero (PLAN §9 P24).
func (s *Store) LogDownload(ctx context.Context, d *Download) error {
	if d.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		d.ID = id
	}
	if d.DownloadedAt.IsZero() {
		d.DownloadedAt = time.Now().UTC().Truncate(time.Second)
	}
	d.DownloadedAt = utcOrZero(d.DownloadedAt)
	_, err := s.db.ExecContext(ctx, `INSERT INTO enclosure_downloads
		(id, entry_id, ip, referrer, user_agent, downloaded_at, enclosure, online)
		VALUES (?,?,?,?,?,?,?,?)`,
		d.ID, d.EntryID, d.IP, d.Referrer, d.UserAgent, d.DownloadedAt, d.Enclosure, d.Online)
	if err != nil {
		return fmt.Errorf("store: log download: %w", err)
	}
	return nil
}

// ListDownloads returns the fetches in a date range, newest first: the
// downloads report (PLAN §9 A27). A zero bound is left open.
func (s *Store) ListDownloads(ctx context.Context, from, to time.Time) ([]Download, error) {
	q := `SELECT id, entry_id, ip, referrer, user_agent, downloaded_at, enclosure, online
		FROM enclosure_downloads`
	var conds []string
	var args []any
	if !from.IsZero() {
		conds = append(conds, "downloaded_at >= ?")
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		conds = append(conds, "downloaded_at <= ?")
		args = append(args, to.UTC())
	}
	for i, c := range conds {
		if i == 0 {
			q += " WHERE " + c
		} else {
			q += " AND " + c
		}
	}
	q += " ORDER BY downloaded_at DESC, id DESC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list downloads: %w", err)
	}
	defer rows.Close()
	var out []Download
	for rows.Next() {
		var d Download
		var at time.Time
		if err := rows.Scan(&d.ID, &d.EntryID, &d.IP, &d.Referrer, &d.UserAgent,
			&at, &d.Enclosure, &d.Online); err != nil {
			return nil, fmt.Errorf("store: list downloads: %w", err)
		}
		d.DownloadedAt = at.UTC()
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list downloads: %w", err)
	}
	return out, nil
}
