package chatintegration

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/yomezza/cosmosimulator/internal/platform/dblayout"
)

// GrantRuntime is an explicit deployment operation performed by a DBA, never by
// Chat on startup. The definer role must be a dedicated unprivileged NOLOGIN role.
func GrantRuntime(ctx context.Context, tx pgx.Tx, schema, runtimeRole, definerRole string) error {
	if runtimeRole == definerRole {
		return fmt.Errorf("separate runtime and definer roles required")
	}
	var unsafe bool
	if e := tx.QueryRow(ctx, `SELECT rolcanlogin OR rolsuper OR rolcreaterole OR rolcreatedb OR rolinherit FROM pg_roles WHERE rolname=$1`, definerRole).Scan(&unsafe); e != nil {
		return e
	}
	if unsafe {
		return fmt.Errorf("definer must be NOLOGIN NOINHERIT NOSUPERUSER NOCREATEROLE NOCREATEDB")
	}
	s := pgx.Identifier{schema}.Sanitize()
	r := pgx.Identifier{runtimeRole}.Sanitize()
	d := pgx.Identifier{definerRole}.Sanitize()
	routed, err := dblayout.Installed(ctx, tx, schema)
	if err != nil {
		return err
	}
	namespaces, definers := s, s
	if routed {
		namespaces = dblayout.QuotedList(dblayout.Names(schema, false))
		definers = pgx.Identifier{dblayout.Schema(schema, "integration")}.Sanitize()
	}
	reference := regexp.MustCompile(regexp.QuoteMeta(s) + `\.(?:"([a-z_][a-z0-9_]*)"|([a-z_][a-z0-9_]*))`)
	exec := func(sql string) error {
		if routed {
			sql = reference.ReplaceAllStringFunc(sql, func(m string) string {
				p := reference.FindStringSubmatch(m)
				return dblayout.Qualified(schema, p[1]+p[2])
			})
		}
		_, e := tx.Exec(ctx, sql)
		return e
	}
	for _, q := range []string{`GRANT USAGE ON SCHEMA ` + namespaces + ` TO ` + r + `,` + d, `GRANT SELECT(token_hash,user_id,expires_at) ON ` + s + `.sessions TO ` + d, `GRANT SELECT(id,username,role,is_bot,company_id,company_role) ON ` + s + `.users TO ` + r + `,` + d, `GRANT SELECT(id,name,tag,owner_id,chat_deleting) ON ` + s + `.companies TO ` + r + `,` + d, `GRANT SELECT(id,company_id,permissions) ON ` + s + `.company_roles TO ` + r + `,` + d, `GRANT SELECT(id,name,user_id) ON ` + s + `.ships TO ` + r, `GRANT SELECT ON ` + s + `.surveys,` + s + `.planet_surfaces,` + s + `.planet_sectors TO ` + r, `GRANT SELECT ON ` + s + `.planet_runtime_leases,` + s + `.planet_runtime_controls,` + s + `.planet_visits TO ` + r + `,` + d, `GRANT SELECT ON ` + s + `.chat_game_moderator_grants TO ` + d, `GRANT SELECT,UPDATE(revision) ON ` + s + `.chat_company_access_fences TO ` + d, `GRANT SELECT ON ` + s + `.chat_channels TO ` + d, `GRANT UPDATE(state) ON ` + s + `.planet_runtime_controls TO ` + d, `GRANT UPDATE(connection_epoch) ON ` + s + `.planet_visits TO ` + d, `GRANT SELECT ON ` + s + `.chat_schema_state,` + s + `.chat_company_deletion_requests,` + s + `.chat_surface_cleanup_requests TO ` + r} {
		if e := exec(q); e != nil {
			return e
		}
	}
	if e := exec(`GRANT SELECT,UPDATE(erasure_count) ON ` + s + `.chat_recovery_state TO ` + r); e != nil {
		return e
	}
	tables := []string{"channels", "conversations", "dm_pair_locks", "dm_pairs", "members", "invites", "invite_rate_locks", "invite_attempts", "message_locator", "messages", "read_receipts", "read_marks", "rate_state", "rate_events", "folders", "saved_items", "user_settings", "notifications", "reports", "audit", "corporation_mutes", "corporation_mute_state", "operations", "outbox", "ws_leases", "company_cleanup_receipts", "surface_cleanup_receipts", "cleanup_jobs"}
	for _, t := range tables {
		if e := exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ` + s + `.` + pgx.Identifier{"chat_" + t}.Sanitize() + ` TO ` + r); e != nil {
			return e
		}
	}
	for _, seq := range []string{"chat_audit_id_seq", "chat_outbox_id_seq"} {
		if e := exec(`GRANT USAGE,SELECT ON SEQUENCE ` + s + `.` + pgx.Identifier{seq}.Sanitize() + ` TO ` + r); e != nil {
			return e
		}
	}
	// ALTER OWNER requires CREATE on the schema; revoke it immediately afterwards.
	if e := exec(`GRANT CREATE ON SCHEMA ` + definers + ` TO ` + d); e != nil {
		return e
	}
	for _, fn := range []string{"chat_validate_session(text)", "chat_lock_company_access(bigint,bigint)", "chat_lock_planet_presence(bigint)", "chat_lock_service_scope(bigint,uuid)", "chat_scope_stamp(bigint)"} {
		if e := exec(`ALTER FUNCTION ` + s + `.` + fn + ` OWNER TO ` + d); e != nil {
			return e
		}
		if e := exec(`REVOKE ALL ON FUNCTION ` + s + `.` + fn + ` FROM PUBLIC`); e != nil {
			return e
		}
		if e := exec(`GRANT EXECUTE ON FUNCTION ` + s + `.` + fn + ` TO ` + r); e != nil {
			return e
		}
	}
	return exec(`REVOKE CREATE ON SCHEMA ` + definers + ` FROM ` + d)
}
