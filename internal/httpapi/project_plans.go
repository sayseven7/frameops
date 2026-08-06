package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

const (
	maxProjectPlanText  = 4000
	maxProjectPlanItems = 128
)

type projectPlanInput struct {
	StartsOn          string                       `json:"startsOn"`
	EndsOn            string                       `json:"endsOn"`
	RulesOfEngagement string                       `json:"rulesOfEngagement"`
	Targets           []string                     `json:"targets"`
	Exclusions        []string                     `json:"exclusions"`
	Team              []postgres.ProjectTeamMember `json:"team"`
	Milestones        []projectMilestoneInput      `json:"milestones"`
}

type projectMilestoneInput struct {
	Title string `json:"title"`
	DueOn string `json:"dueOn"`
}

func (server Server) projectPlan(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/plan")
	if !ok {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method != http.MethodGet)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		plan, err := postgres.ReadProjectPlan(request.Context(), server.pool, session, engagementID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, plan)
	case http.MethodPost, http.MethodPut:
		plan, ok := decodeProjectPlan(response, request)
		if !ok {
			return
		}
		plan.EngagementID = engagementID
		if len(plan.Team) == 0 {
			plan.Team = []postgres.ProjectTeamMember{{UserID: session.UserID, Role: "lead"}}
		}
		var err error
		if request.Method == http.MethodPost {
			plan, err = postgres.CreateProjectPlan(request.Context(), server.pool, session, plan)
		} else {
			plan, err = postgres.UpdateProjectPlan(request.Context(), server.pool, session, plan)
		}
		switch {
		case errors.Is(err, postgres.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found")
		case errors.Is(err, postgres.ErrInvalidState):
			writeError(response, http.StatusConflict, "invalid_state")
		case err != nil:
			writeError(response, http.StatusInternalServerError, "internal_error")
		default:
			if request.Method == http.MethodPost {
				writeJSON(response, http.StatusCreated, plan)
			} else {
				writeJSON(response, http.StatusOK, plan)
			}
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) projectPlanTransition(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/plan/transition")
	if !ok || request.Method != http.MethodPost {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSON(request, &input) || (input.Status != "active" && input.Status != "closed") {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	plan, err := postgres.TransitionProjectPlan(request.Context(), server.pool, session, engagementID, input.Status)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(response, http.StatusConflict, "invalid_state")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
	default:
		writeJSON(response, http.StatusOK, plan)
	}
}

func decodeProjectPlan(response http.ResponseWriter, request *http.Request) (postgres.ProjectPlan, bool) {
	var input projectPlanInput
	if !decodeJSON(request, &input) || len(input.Targets) == 0 || len(input.Targets) > maxProjectPlanItems || len(input.Exclusions) > maxProjectPlanItems || len(input.Team) > 64 || len(input.Milestones) > maxProjectPlanItems {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return postgres.ProjectPlan{}, false
	}
	startsOn, startErr := time.Parse("2006-01-02", input.StartsOn)
	endsOn, endErr := time.Parse("2006-01-02", input.EndsOn)
	plan := postgres.ProjectPlan{StartsOn: startsOn, EndsOn: endsOn, RulesOfEngagement: strings.TrimSpace(input.RulesOfEngagement), Scope: postgres.ProjectScope{Targets: trimProjectPlanStrings(input.Targets), Exclusions: trimProjectPlanStrings(input.Exclusions)}, Team: input.Team}
	if startErr != nil || endErr != nil || plan.EndsOn.Before(plan.StartsOn) || len(plan.RulesOfEngagement) == 0 || len(plan.RulesOfEngagement) > maxProjectPlanText {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return postgres.ProjectPlan{}, false
	}
	for _, target := range append(append([]string{}, plan.Scope.Targets...), plan.Scope.Exclusions...) {
		if len(target) == 0 || len(target) > 500 {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return postgres.ProjectPlan{}, false
		}
	}
	for _, milestone := range input.Milestones {
		dueOn, err := time.Parse("2006-01-02", milestone.DueOn)
		if err != nil || strings.TrimSpace(milestone.Title) == "" || len(milestone.Title) > 200 || dueOn.Before(plan.StartsOn) || dueOn.After(plan.EndsOn) {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return postgres.ProjectPlan{}, false
		}
		plan.Milestones = append(plan.Milestones, postgres.ProjectMilestone{Title: strings.TrimSpace(milestone.Title), DueOn: dueOn})
	}
	return plan, true
}

func trimProjectPlanStrings(items []string) []string {
	for index := range items {
		items[index] = strings.TrimSpace(items[index])
	}
	return items
}
