package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/sayseven7/frameops/internal/reportdocx"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

const structuredReportTemplateVersion = "frameops-structured-v1"

// generateReportRevision snapshots the structured records already owned by an
// engagement, then stores the generated DOCX through the same immutable revision
// path as an editorially imported file.
func (server Server) generateReportRevision(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/reports/generate")
	if !ok || request.Method != http.MethodPost {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	contents, err := server.withStructuredReportSnapshot(request.Context(), func(tx pgx.Tx) ([]byte, error) {
		source, err := server.structuredReportSource(request.Context(), tx, session, engagementID)
		if err != nil {
			return nil, err
		}
		return reportdocx.Generate(source)
	})
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	digest := sha256.Sum256(contents)
	revision, err := postgres.ReserveReportRevision(request.Context(), server.pool, session, engagementID, structuredReportTemplateVersion+".docx", hex.EncodeToString(digest[:]), int64(len(contents)))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	if err != nil || server.evidence.Put(request.Context(), revision.StorageKey, bytes.NewReader(contents), int64(len(contents)), revision.SHA256, "application/vnd.openxmlformats-officedocument.wordprocessingml.document") != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	revision, err = postgres.ConfirmReportRevision(request.Context(), server.pool, session, revision.ID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(response, http.StatusCreated, revision)
}

func (server Server) withStructuredReportSnapshot(ctx context.Context, generate func(pgx.Tx) ([]byte, error)) ([]byte, error) {
	tx, err := server.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin structured report snapshot: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	return generate(tx)
}

func (server Server) structuredReportSource(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session postgres.Session, engagementID string) (reportdocx.Source, error) {
	var source reportdocx.Source
	source.TemplateVersion = structuredReportTemplateVersion
	if err := pool.QueryRow(ctx, `SELECT clients.name, engagements.name FROM engagements JOIN clients ON clients.organization_id = engagements.organization_id AND clients.id = engagements.client_id WHERE engagements.organization_id = $1 AND engagements.id = $2`, session.OrganizationID, engagementID).Scan(&source.ClientName, &source.EngagementName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reportdocx.Source{}, postgres.ErrNotFound
		}
		return reportdocx.Source{}, fmt.Errorf("read report engagement: %w", err)
	}
	if plan, err := postgres.ReadProjectPlan(ctx, pool, session, engagementID); err == nil {
		source.Scope = append(source.Scope, plan.Scope.Targets...)
		source.Scope = append(source.Scope, "Rules of engagement: "+plan.RulesOfEngagement)
	} else if !errors.Is(err, postgres.ErrNotFound) {
		return reportdocx.Source{}, err
	}
	if checklist, err := postgres.ReadEngagementChecklist(ctx, pool, session, engagementID); err == nil {
		for _, item := range checklist.Items {
			source.Methodology = append(source.Methodology, item.Title+": "+item.Procedure)
		}
	} else if !errors.Is(err, postgres.ErrNotFound) {
		return reportdocx.Source{}, err
	}
	findings, err := postgres.ListFindings(ctx, pool, session, engagementID)
	if err != nil {
		return reportdocx.Source{}, err
	}
	for _, finding := range findings {
		entry := reportdocx.Finding{Title: finding.Title, CVSS: fmt.Sprintf("%.1f", finding.CVSSScore)}
		evidence, err := postgres.ListEvidence(ctx, pool, session, finding.ID)
		if err != nil {
			return reportdocx.Source{}, err
		}
		for _, item := range evidence {
			if item.State == "stored" {
				entry.Evidence = append(entry.Evidence, item.Filename+" SHA-256 "+item.SHA256)
			}
		}
		retests, err := postgres.ListRetests(ctx, pool, session, finding.ID)
		if err != nil {
			return reportdocx.Source{}, err
		}
		for _, item := range retests {
			entry.Retests = append(entry.Retests, fmt.Sprintf("Round %d: %s — %s", item.Round, item.ResultState, item.ObservedResult))
		}
		source.Findings = append(source.Findings, entry)
	}
	return source, nil
}
