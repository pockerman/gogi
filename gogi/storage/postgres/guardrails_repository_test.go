package postgres

import (
	"context"
	"errors"
	"gogi/gogi/guardrails"
	"os"
	"testing"
	"time"
)

// The test runs against a migrated database, e.g.
// GOGI_TEST_POSTGRES_DSN=postgres://postgres:test@localhost:5432/gogi?sslmode=disable
func newTestGuardrailsRepository(t *testing.T) *GogiGuardrailsRepository {
	dsn := os.Getenv("GOGI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOGI_TEST_POSTGRES_DSN is not set")
	}

	pool, err := NewPool(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// the tables are emptied before and after the test, so use a dedicated test database

	clear := func() {
		for _, table := range []string{"gogi_guardrail_policies", "gogi_guardrail_evaluations", "gogi_guardrail_violations"} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM "+table); err != nil {
				t.Fatal(err)
			}
		}
	}
	clear()
	t.Cleanup(clear)
	return NewGogiGuardrailsRepository(pool)
}

func TestGuardrailPolicies(t *testing.T) {
	repo := newTestGuardrailsRepository(t)
	ctx := context.Background()

	if _, err := repo.GetPolicy(ctx, "intake-input"); !errors.Is(err, guardrails.ErrPolicyNotFound) {
		t.Errorf("expected ErrPolicyNotFound, got %v", err)
	}

	policy := guardrails.Policy{Name: "intake-input", Type: guardrails.TypeInput, ApplicationID: "patient-intake",
		Mode: guardrails.ModeEnforce, RulesJSON: `[{"type": "prompt_injection"}]`, Version: "1.0.0",
		Source: guardrails.SourceFile}
	added, err := repo.UpsertPolicy(ctx, policy)
	if err != nil || added.Revision != 1 || added.CreatedAt.IsZero() {
		t.Fatalf("wrong policy %+v, %v", added, err)
	}

	// the same definition, formatted differently, changes nothing
	policy.RulesJSON = `[{"type":"prompt_injection"}]`
	same, err := repo.UpsertPolicy(ctx, policy)
	if err != nil || same.Revision != 1 {
		t.Errorf("an unchanged policy should keep its revision, got %+v, %v", same, err)
	}

	policy.Mode = guardrails.ModeShadow
	changed, err := repo.UpsertPolicy(ctx, policy)
	if err != nil || changed.Revision != 2 || changed.Mode != guardrails.ModeShadow ||
		!changed.CreatedAt.Equal(added.CreatedAt) {
		t.Errorf("a changed policy should get a new revision, got %+v, %v", changed, err)
	}

	if _, err := repo.UpsertPolicy(ctx, guardrails.Policy{Name: "billing-output", Type: guardrails.TypeOutput,
		ApplicationID: "billing", Mode: guardrails.ModeEnforce, RulesJSON: `[{"type":"pii_detection"}]`,
		Source: guardrails.SourceAPI}); err != nil {
		t.Fatal(err)
	}
	all, _ := repo.ListPolicies(ctx, "", "")
	billing, _ := repo.ListPolicies(ctx, "billing", "")
	inputs, _ := repo.ListPolicies(ctx, "", guardrails.TypeInput)
	if len(all) != 2 || all[0].Name != "billing-output" || len(billing) != 1 || len(inputs) != 1 ||
		inputs[0].Name != "intake-input" {
		t.Errorf("wrong lists %d %d %d", len(all), len(billing), len(inputs))
	}
	if compiled, _, err := guardrails.Compile(inputs[0]); err != nil || compiled == nil {
		t.Errorf("a stored policy should compile, got %v", err)
	}

	deleted, _ := repo.DeletePolicy(ctx, "billing-output")
	again, _ := repo.DeletePolicy(ctx, "billing-output")
	if !deleted || again {
		t.Errorf("wrong deletions %v %v", deleted, again)
	}
}

func TestGuardrailEvaluationsAndViolations(t *testing.T) {
	repo := newTestGuardrailsRepository(t)
	ctx := context.Background()

	for _, evaluation := range []guardrails.Evaluation{
		{InspectionPoint: guardrails.PointAction, Action: "book", Outcome: guardrails.OutcomeAllowed, SessionID: "s-1", TurnID: "t-1"},
		{InspectionPoint: guardrails.PointAction, Action: "cancel", Outcome: guardrails.OutcomeBlocked, SessionID: "s-1"},
		{InspectionPoint: guardrails.PointAction, Action: "cancel", Outcome: guardrails.OutcomeAllowed, SessionID: "s-1", TurnID: "t-2",
			Triggered:   []guardrails.TriggeredRule{{PolicyName: "p", RuleType: "max_per_session", Shadow: true}},
			PolicyNames: []string{"p"}, Context: map[string]string{"user_id": "p-42"}},
		{InspectionPoint: guardrails.PointInput, Outcome: guardrails.OutcomeAllowed, SessionID: "s-1"},
		{InspectionPoint: guardrails.PointAction, Action: "book", Outcome: guardrails.OutcomeAllowed, SessionID: "s-2"},
	} {
		if id, err := repo.RecordEvaluation(ctx, evaluation); err != nil || id == "" {
			t.Fatalf("failed to record %+v: %v", evaluation, err)
		}
	}

	actions, err := repo.SessionActions(ctx, "s-1")
	if err != nil || len(actions) != 2 || actions[0] != (guardrails.PastAction{Action: "book", TurnID: "t-1"}) ||
		actions[1] != (guardrails.PastAction{Action: "cancel", TurnID: "t-2"}) {
		t.Errorf("wrong session actions %+v, %v", actions, err)
	}

	start := time.Now().Add(-time.Minute)
	for _, severity := range []string{"low", "blocked", "high"} {
		if _, err := repo.RecordViolation(ctx, guardrails.Violation{PolicyName: "booking-rules", Action: "book",
			Severity: severity, Context: map[string]string{"user_id": "p-42"}, Details: "limit"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.RecordViolation(ctx, guardrails.Violation{PolicyName: "other", Severity: "low"}); err != nil {
		t.Fatal(err)
	}

	violations, err := repo.ListViolations(ctx, guardrails.ViolationFilter{PolicyName: "booking-rules", Since: start})
	if err != nil || len(violations) != 3 || violations[0].Severity != "high" || violations[2].Context["user_id"] != "p-42" {
		t.Errorf("wrong violations %+v, %v", violations, err)
	}
	limited, _ := repo.ListViolations(ctx, guardrails.ViolationFilter{Severity: "low", Limit: 1})
	if len(limited) != 1 || limited[0].PolicyName != "other" {
		t.Errorf("wrong limited violations %+v", limited)
	}
	future, _ := repo.ListViolations(ctx, guardrails.ViolationFilter{Since: time.Now().Add(time.Hour)})
	if len(future) != 0 {
		t.Errorf("expected no violations, got %d", len(future))
	}
}
