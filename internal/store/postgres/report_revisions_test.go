package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestReportApprovalUniqueViolation(t *testing.T) {
	for name, err := range map[string]error{
		"expected approval index":   &pgconn.PgError{Code: "23505", ConstraintName: "report_revisions_one_approved_per_engagement_idx"},
		"other unique index":        &pgconn.PgError{Code: "23505", ConstraintName: "report_revisions_storage_key_key"},
		"unexpected database error": &pgconn.PgError{Code: "23514"},
		"wrapped expected index":    fmt.Errorf("approve: %w", &pgconn.PgError{Code: "23505", ConstraintName: "report_revisions_one_approved_per_engagement_idx"}),
	} {
		t.Run(name, func(t *testing.T) {
			want := name == "expected approval index" || name == "wrapped expected index"
			if got := isReportApprovalUniqueViolation(err); got != want {
				t.Fatalf("isReportApprovalUniqueViolation(%v) = %t, want %t", err, got, want)
			}
		})
	}

	if isReportApprovalUniqueViolation(errors.New("connection lost")) {
		t.Fatal("non-database error was treated as an approval conflict")
	}
}
