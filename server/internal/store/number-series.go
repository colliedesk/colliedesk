package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// DefaultNumberSeries is the series a conversation is numbered in unless its channel names another.
const DefaultNumberSeries = "default"

// NumberSeries is a series conversations are numbered in: the code format, the number the next one
// takes, and where this server took it over (a code below start, in start_year or before, that is not
// here belongs to the system before).
type NumberSeries struct {
	Series    string    `json:"series"`
	Format    string    `json:"format"`
	Next      int64     `json:"next"`
	Start     int64     `json:"start"`
	StartYear *int      `json:"start_year"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NumberSeriesInput adds a series or changes one: Next only counts on.
type NumberSeriesInput struct {
	Format    *string
	Next      *int64
	Start     *int64
	StartYear *int
}

var seriesKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,19}$`)

// ErrNumberSeries refuses a change of a series, saying why.
type ErrNumberSeries struct{ Field, Message string }

func (e *ErrNumberSeries) Error() string { return e.Message }

// ListNumberSeries lists the workspace's series, the default one first.
func (s *Store) ListNumberSeries(ctx context.Context, workspaceID string) ([]NumberSeries, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]NumberSeries, error) {
		if _, err := tx.Exec(ctx, `WITH series AS (INSERT INTO conversation_number_series(workspace_id,series,format) VALUES($1,$2,$3) ON CONFLICT DO NOTHING RETURNING 1)
			INSERT INTO conversation_number_formats(workspace_id,series,format) SELECT $1,$2,$3 FROM series ON CONFLICT DO NOTHING`,
			workspaceID, DefaultNumberSeries, domain.DefaultNumberFormat); err != nil {
			return nil, fmt.Errorf("add the default number series: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT series,format,next,start,start_year,updated_at FROM conversation_number_series WHERE workspace_id=$1
			ORDER BY series<>$2,series`, workspaceID, DefaultNumberSeries)
		if err != nil {
			return nil, fmt.Errorf("list number series: %w", err)
		}
		return pgx.CollectRows(rows, scanNumberSeries)
	})
}

func scanNumberSeries(row pgx.CollectableRow) (NumberSeries, error) {
	var series NumberSeries
	err := row.Scan(&series.Series, &series.Format, &series.Next, &series.Start, &series.StartYear, &series.UpdatedAt)
	series.UpdatedAt = domain.UTC(series.UpdatedAt)
	return series, err
}

// SetNumberSeries adds a series or changes its format, next number or start. The next number never
// goes back, and never below a number the series gave in the current year.
func (s *Store) SetNumberSeries(ctx context.Context, workspaceID, key string, input NumberSeriesInput) (NumberSeries, error) {
	if !seriesKey.MatchString(key) {
		return NumberSeries{}, &ErrNumberSeries{Field: "series", Message: "A series is a lowercase name: letters, digits, - and _, 20 at most."}
	}
	if input.Format != nil {
		if _, err := domain.ParseNumberFormat(*input.Format); err != nil {
			return NumberSeries{}, &ErrNumberSeries{Field: "format", Message: "Format: " + err.Error() + "."}
		}
	}
	for field, value := range map[string]*int64{"next": input.Next, "start": input.Start} {
		if value != nil && *value < 1 {
			return NumberSeries{}, &ErrNumberSeries{Field: field, Message: field + " is 1 or more."}
		}
	}
	if input.StartYear != nil && (*input.StartYear < 1970 || *input.StartYear > 9999) {
		return NumberSeries{}, &ErrNumberSeries{Field: "start_year", Message: "start_year is a year."}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (NumberSeries, error) {
		var current NumberSeries
		err := tx.QueryRow(ctx, `SELECT series,format,next,start,start_year,updated_at FROM conversation_number_series WHERE workspace_id=$1 AND series=$2 FOR UPDATE`,
			workspaceID, key).Scan(&current.Series, &current.Format, &current.Next, &current.Start, &current.StartYear, &current.UpdatedAt)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			format := domain.DefaultNumberFormat
			if input.Format != nil {
				format = *input.Format
			} else if key != DefaultNumberSeries {
				return NumberSeries{}, &ErrNumberSeries{Field: "format", Message: "A new series needs its format."}
			}
			current = NumberSeries{Series: key, Format: format, Next: 1, Start: 1}
			if _, err := tx.Exec(ctx, `INSERT INTO conversation_number_series(workspace_id,series,format) VALUES($1,$2,$3)`, workspaceID, key, format); err != nil {
				return NumberSeries{}, fmt.Errorf("add number series: %w", err)
			}
		case err != nil:
			return NumberSeries{}, fmt.Errorf("read number series: %w", err)
		}
		if input.Next != nil {
			var used int64
			if err := tx.QueryRow(ctx, `SELECT COALESCE(max(number),0) FROM conversations WHERE workspace_id=$1 AND number_series=$2`, workspaceID, key).Scan(&used); err != nil {
				return NumberSeries{}, fmt.Errorf("read the series' numbers: %w", err)
			}
			if *input.Next < current.Next || *input.Next <= used {
				return NumberSeries{}, &ErrNumberSeries{Field: "next", Message: fmt.Sprintf("The next number only counts on: use %d or more.", max(current.Next, used+1))}
			}
		}
		// Every format the series has had reads the codes it gave (conversationsByCode).
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_number_formats(workspace_id,series,format) VALUES($1,$2,COALESCE($3,$4)) ON CONFLICT DO NOTHING`,
			workspaceID, key, input.Format, current.Format); err != nil {
			return NumberSeries{}, fmt.Errorf("keep the series' format: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE conversation_number_series SET format=COALESCE($3,format),next=COALESCE($4,next),start=COALESCE($5,start),
			start_year=CASE WHEN $6::boolean THEN $7 ELSE start_year END,updated_at=now() WHERE workspace_id=$1 AND series=$2`,
			workspaceID, key, input.Format, input.Next, input.Start, input.StartYear != nil, input.StartYear); err != nil {
			return NumberSeries{}, fmt.Errorf("change number series: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT series,format,next,start,start_year,updated_at FROM conversation_number_series WHERE workspace_id=$1 AND series=$2`, workspaceID, key)
		if err != nil {
			return NumberSeries{}, err
		}
		return pgx.CollectExactlyOneRow(rows, scanNumberSeries)
	})
}
