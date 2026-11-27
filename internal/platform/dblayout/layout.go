// Package dblayout defines one installation's trusted, schema-qualified object
// ownership. A database may contain several independently rooted installations.
package dblayout

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var Domains = []string{"identity", "universe", "fleet", "economy", "corporations", "colonies", "planet", "chat", "knowledge", "integration", "ops", "dictionaries", "archive"}
var tables = map[string]string{}

func init() {
	groups := map[string]string{
		"dictionaries": "system_names planet_names station_names asteroid_names ice_field_names anomaly_names name_dictionary_state",
		"identity":     "users sessions notifications reputation pilot_workspace_preferences pilot_workspace_ids",
		"universe":     "universe_state galaxy_catalog system_identity star_systems system_planets system_objects universe_lanes universe_regions portal portal_contributions",
		"fleet":        "ship_module_catalog ship_module_hulls ship_modules ships stations navigation_notes automation_tasks logistics_operations",
		"economy":      "station_module_stock local_markets expedition_command_receipts deposits mining_jobs reservations resource_quotas financial_ledger warehouses sell_orders deliveries shipyard_stock survey_offers",
		"corporations": "companies company_roles company_requests company_finance company_projects project_contributions company_storage",
		"colonies":     "planet_settings planet_surfaces planet_sectors planet_projects planet_contributions planet_events planet_requests planet_refunds planet_colonies planet_surface_controls planet_runtime_controls planet_landing_sites aj_colony_names aj_colony_name_history",
		"planet":       "planet_runtime_leases planet_local_locations planet_characters planet_visits planet_handoff_uses planet_transition_receipts planet_sector_buildings aj_planet_recovery_diagnostics",
		"integration":  "planet_transfer_controls planet_handoffs planet_session_lookup aj_planet_return_authorizations",
		"knowledge":    "surveys research_jobs aj_discovery_objects aj_discovery_name_history aj_guide_categories aj_guide_category_i18n aj_guide_articles aj_guide_article_revisions aj_guide_publications aj_guide_publication_history aj_guide_related aj_guide_state aj_guide_operations aj_guide_media aj_guide_revision_media",
		"ops":          "world game_settings game_release game_events admin_log benchmark_runs player_ideas planet_schema_state",
		"archive":      "galaxies",
	}
	for domain, list := range groups {
		for _, name := range strings.Fields(list) {
			tables[name] = domain
		}
	}
}

var simpleRoot = regexp.MustCompile(`^[a-z][a-z0-9_]{0,29}$`)

func Prefix(root string) string {
	if simpleRoot.MatchString(root) {
		return root
	}
	hash := sha256.Sum256([]byte(root))
	return "aj_" + hex.EncodeToString(hash[:12])
}
func Schema(root, domain string) string { return Prefix(root) + "_" + domain }
func Domain(name string) string {
	if d := tables[name]; d != "" {
		return d
	}
	if strings.HasPrefix(name, "star_systems_p_") {
		return "universe"
	}
	switch name {
	case "chat_validate_session", "chat_lock_company_access", "chat_lock_planet_presence", "chat_lock_service_scope", "chat_scope_stamp", "chat_touch_company_fence":
		return "integration"
	case "ledger_credit_change", "enforce_credit_ceiling":
		return "economy"
	case "event_notification":
		return "ops"
	case "ship_module_initial_loadout":
		return "fleet"
	case "planet_reserved_ship":
		return "integration"
	case "planet_territory_dirty":
		return "colonies"
	case "universe_legacy_write_guard":
		return "archive"
	}
	if strings.HasPrefix(name, "chat_") {
		return "chat"
	}
	if strings.HasPrefix(name, "aj_guide_") || strings.HasPrefix(name, "aj_discovery_") || name == "aj_name_history_immutable" {
		return "knowledge"
	}
	if strings.HasPrefix(name, "universe_") {
		return "universe"
	}
	return ""
}
func Qualified(root, name string) string {
	if d := Domain(name); d != "" {
		return pgx.Identifier{Schema(root, d), name}.Sanitize()
	}
	return pgx.Identifier{root, name}.Sanitize()
}
func Names(root string, archive bool) []string {
	out := []string{root}
	for _, d := range Domains {
		if d != "archive" || archive {
			out = append(out, Schema(root, d))
		}
	}
	return out
}
func QuotedList(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = pgx.Identifier{n}.Sanitize()
	}
	return strings.Join(out, ",")
}
func SearchPath(root string) string { return QuotedList(Names(root, false)) }
func FunctionPath(root string) string {
	names := Names(root, true)[1:]
	return "pg_catalog," + QuotedList(names) + ",pg_temp"
}
func MigrationPath(root string) string {
	names := Names(root, true)
	return QuotedList(append(names[1:], root))
}
func DropList(root string) string { return QuotedList(Names(root, true)) }

// RouteDDL qualifies creation targets, including trusted CREATE VIEW strings in
// historical migration DO blocks. Domain identifiers are always safe ASCII.
var creates = regexp.MustCompile(`(?i)(CREATE\s+(?:OR\s+REPLACE\s+)?(?:UNLOGGED\s+)?(?:TABLE|VIEW|FUNCTION|COLLATION|SEQUENCE|TYPE)\s+(?:IF\s+NOT\s+EXISTS\s+)?)([a-z_][a-z0-9_]*)(\s|\()`)
var languages = regexp.MustCompile(`(?i)(LANGUAGE\s+(?:plpgsql|sql))(\s+AS\s+)`)

func DDL(root, sql string) string {
	sql = creates.ReplaceAllStringFunc(sql, func(m string) string {
		parts := creates.FindStringSubmatch(m)
		if Domain(parts[2]) == "" {
			return m
		}
		return parts[1] + Qualified(root, parts[2]) + parts[3]
	})
	return languages.ReplaceAllString(sql, "${1} SET search_path="+FunctionPath(root)+"${2}")
}
