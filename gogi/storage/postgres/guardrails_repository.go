package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gogi/gogi/guardrails"
	"gogi/gogi/utils"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const GOGI_GUARDRAILS_TABLE_NAME string = "gogi_guardrail_policies"

const guardrailPolicyColumns = `name, type, application_id, mode, rules, description, version, revision, source,
	created_at, updated_at`

var _ guardrails.Store = (*GogiGuardrailsRepository)(nil)

// GogiGuardrailsRepository stores guardrail policies and the audit records of their
// evaluations and of reported violations. It implements guardrails.Store
type GogiGuardrailsRepository struct {
	pool *pgxpool.Pool
}

func NewGogiGuardrailsRepository(
	pool *pgxpool.Pool,
) *GogiGuardrailsRepository {

	return &GogiGuardrailsRepository{
		pool: pool,
	}
}

// UpsertPolicy adds the policy, or replaces the one with the same name. The revision is
// incremented only if the definition changed, so loading the same policy files on every
// start, by every replica, leaves policies as they are
func (r *GogiGuardrailsRepository) UpsertPolicy(ctx context.Context, policy guardrails.Policy) (guardrails.Policy, error) {

	row := r.pool.QueryRow(ctx,
		`INSERT INTO gogi_guardrail_policies (name, type, application_id, mode, rules, description, version, source)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		 ON CONFLICT (name) DO UPDATE SET
		     type = EXCLUDED.type,
		     application_id = EXCLUDED.application_id,
		     mode = EXCLUDED.mode,
		     rules = EXCLUDED.rules,
		     description = EXCLUDED.description,
		     version = EXCLUDED.version,
		     source = EXCLUDED.source,
		     revision = gogi_guardrail_policies.revision + 1,
		     updated_at = NOW()
		 WHERE (gogi_guardrail_policies.type, gogi_guardrail_policies.application_id, gogi_guardrail_policies.mode,
		        gogi_guardrail_policies.rules, gogi_guardrail_policies.description, gogi_guardrail_policies.version,
		        gogi_guardrail_policies.source)
		       IS DISTINCT FROM
		       (EXCLUDED.type, EXCLUDED.application_id, EXCLUDED.mode, EXCLUDED.rules, EXCLUDED.description,
		        EXCLUDED.version, EXCLUDED.source)
		 RETURNING `+guardrailPolicyColumns,
		policy.Name, policy.Type, policy.ApplicationID, policy.Mode, policy.RulesJSON, policy.Description,
		policy.Version, policy.Source)

	stored, err := scanGuardrailPolicy(row)
	// nothing changed, so nothing was returned
	if errors.Is(err, pgx.ErrNoRows) {
		return r.GetPolicy(ctx, policy.Name)
	}
	return stored, err
}

func (r *GogiGuardrailsRepository) GetPolicy(ctx context.Context, name string) (guardrails.Policy, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+guardrailPolicyColumns+` FROM gogi_guardrail_policies WHERE name = $1`, name)

	policy, err := scanGuardrailPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return guardrails.Policy{}, fmt.Errorf("%w: %s", guardrails.ErrPolicyNotFound, name)
	}
	return policy, err
}

func (r *GogiGuardrailsRepository) ListPolicies(ctx context.Context, applicationID, policyType string) ([]guardrails.Policy, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+guardrailPolicyColumns+` FROM gogi_guardrail_policies
		 WHERE ($1 = '' OR application_id = $1) AND ($2 = '' OR type = $2)
		 ORDER BY name`, applicationID, policyType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	policies := make([]guardrails.Policy, 0)
	for rows.Next() {
		policy, err := scanGuardrailPolicy(rows)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (r *GogiGuardrailsRepository) DeletePolicy(ctx context.Context, name string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM gogi_guardrail_policies WHERE name = $1`, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func scanGuardrailPolicy(row pgx.Row) (guardrails.Policy, error) {
	var policy guardrails.Policy
	var rules []byte
	err := row.Scan(&policy.Name, &policy.Type, &policy.ApplicationID, &policy.Mode, &rules, &policy.Description,
		&policy.Version, &policy.Revision, &policy.Source, &policy.CreatedAt, &policy.UpdatedAt)
	policy.RulesJSON = string(rules)
	return policy, err
}

func (r *GogiGuardrailsRepository) RecordEvaluation(ctx context.Context, evaluation guardrails.Evaluation) (string, error) {

	triggered, err := json.Marshal(evaluation.Triggered)
	if err != nil {
		return "", err
	}
	contextJSON, err := json.Marshal(nonNilContext(evaluation.Context))
	if err != nil {
		return "", err
	}
	policyNames := evaluation.PolicyNames
	if policyNames == nil {
		policyNames = []string{}
	}

	id := utils.NewUUIDString()
	_, err = r.pool.Exec(ctx,
		`INSERT INTO gogi_guardrail_evaluations (id, inspection_point, application_id, policy_names, action, outcome,
		     triggered_rules, content_sha256, session_id, turn_id, user_id, context)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12::jsonb)`,
		id, evaluation.InspectionPoint, evaluation.ApplicationID, policyNames, evaluation.Action, evaluation.Outcome,
		string(triggered), evaluation.ContentSHA256, evaluation.SessionID, evaluation.TurnID, evaluation.UserID,
		string(contextJSON))
	if err != nil {
		return "", err
	}
	return id, nil
}

func (r *GogiGuardrailsRepository) SessionActions(ctx context.Context, sessionID string) ([]guardrails.PastAction, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT action, turn_id FROM gogi_guardrail_evaluations
		 WHERE session_id = $1 AND inspection_point = $2 AND outcome = $3
		 ORDER BY created_at`, sessionID, guardrails.PointAction, guardrails.OutcomeAllowed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	actions := make([]guardrails.PastAction, 0)
	for rows.Next() {
		var action guardrails.PastAction
		if err := rows.Scan(&action.Action, &action.TurnID); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (r *GogiGuardrailsRepository) RecordViolation(ctx context.Context, violation guardrails.Violation) (guardrails.Violation, error) {

	contextJSON, err := json.Marshal(nonNilContext(violation.Context))
	if err != nil {
		return guardrails.Violation{}, err
	}

	violation.ID = utils.NewUUIDString()
	err = r.pool.QueryRow(ctx,
		`INSERT INTO gogi_guardrail_violations (id, policy_name, action, severity, context, details, evaluation_id)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
		 RETURNING created_at`,
		violation.ID, violation.PolicyName, violation.Action, violation.Severity, string(contextJSON),
		violation.Details, violation.EvaluationID,
	).Scan(&violation.CreatedAt)
	if err != nil {
		return guardrails.Violation{}, err
	}
	return violation, nil
}

func (r *GogiGuardrailsRepository) ListViolations(ctx context.Context, filter guardrails.ViolationFilter) ([]guardrails.Violation, error) {

	conditions := []string{"TRUE"}
	args := make([]any, 0)
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if filter.PolicyName != "" {
		add("policy_name = $%d", filter.PolicyName)
	}
	if filter.Severity != "" {
		add("severity = $%d", filter.Severity)
	}
	if !filter.Since.IsZero() {
		add("created_at >= $%d", filter.Since)
	}
	query := `SELECT id, policy_name, action, severity, context, details, evaluation_id, created_at
		 FROM gogi_guardrail_violations WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	violations := make([]guardrails.Violation, 0)
	for rows.Next() {
		var violation guardrails.Violation
		var contextJSON []byte
		var createdAt time.Time
		if err := rows.Scan(&violation.ID, &violation.PolicyName, &violation.Action, &violation.Severity,
			&contextJSON, &violation.Details, &violation.EvaluationID, &createdAt); err != nil {
			return nil, err
		}
		violation.CreatedAt = createdAt
		if err := json.Unmarshal(contextJSON, &violation.Context); err != nil {
			return nil, err
		}
		violations = append(violations, violation)
	}
	return violations, rows.Err()
}

func nonNilContext(context map[string]string) map[string]string {
	if context == nil {
		return map[string]string{}
	}
	return context
}
