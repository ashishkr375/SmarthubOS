package queries

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertRule creates a new rule for a tenant.
func InsertRule(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID, ruleJSON json.RawMessage) (*Rule, error) {
	var r Rule
	err := pool.QueryRow(ctx,
		`INSERT INTO rules (tenant_id, rule_json)
		 VALUES ($1, $2)
		 RETURNING id, tenant_id, rule_json, is_active, created_at`,
		tenantID, ruleJSON,
	).Scan(&r.ID, &r.TenantID, &r.RuleJSON, &r.IsActive, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert rule: %w", err)
	}
	return &r, nil
}

// GetRuleByID fetches a single rule scoped to tenantID. Returns pgx.ErrNoRows if missing.
func GetRuleByID(ctx context.Context, pool *pgxpool.Pool, id, tenantID uuid.UUID) (*Rule, error) {
	var r Rule
	err := pool.QueryRow(ctx,
		`SELECT id, tenant_id, rule_json, is_active, created_at
		 FROM rules WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	).Scan(&r.ID, &r.TenantID, &r.RuleJSON, &r.IsActive, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRulesByTenantID returns all rules for a tenant.
func ListRulesByTenantID(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID) ([]Rule, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, tenant_id, rule_json, is_active, created_at
		 FROM rules WHERE tenant_id = $1 ORDER BY created_at DESC`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.TenantID, &r.RuleJSON, &r.IsActive, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// UpdateRule replaces rule_json and is_active for an existing rule.
func UpdateRule(ctx context.Context, pool *pgxpool.Pool, id, tenantID uuid.UUID, ruleJSON json.RawMessage, isActive bool) (*Rule, error) {
	var r Rule
	err := pool.QueryRow(ctx,
		`UPDATE rules SET rule_json = $3, is_active = $4
		 WHERE id = $1 AND tenant_id = $2
		 RETURNING id, tenant_id, rule_json, is_active, created_at`,
		id, tenantID, ruleJSON, isActive,
	).Scan(&r.ID, &r.TenantID, &r.RuleJSON, &r.IsActive, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteRule permanently removes a rule. Returns pgx.ErrNoRows if not found in tenant.
func DeleteRule(ctx context.Context, pool *pgxpool.Pool, id, tenantID uuid.UUID) error {
	tag, err := pool.Exec(ctx,
		`DELETE FROM rules WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// GetActiveRulesForCacheSync returns all active rules for tenant(s) served by hubID.
func GetActiveRulesForCacheSync(ctx context.Context, pool *pgxpool.Pool, hubID uuid.UUID) ([]CacheSyncRule, error) {
	rows, err := pool.Query(ctx,
		`SELECT r.id, r.tenant_id, r.rule_json, r.is_active
		 FROM rules r
		 WHERE r.tenant_id IN (
		     SELECT DISTINCT tenant_id FROM devices WHERE hub_id = $1
		 ) AND r.is_active = TRUE`,
		hubID)
	if err != nil {
		return nil, fmt.Errorf("query cache sync rules: %w", err)
	}
	defer rows.Close()

	var rules []CacheSyncRule
	for rows.Next() {
		var r CacheSyncRule
		if err := rows.Scan(&r.RuleID, &r.TenantID, &r.RuleJSON, &r.IsActive); err != nil {
			return nil, fmt.Errorf("scan cache sync rule: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}
