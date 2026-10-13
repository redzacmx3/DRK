package app

import (
	"context"
	"database/sql"
)

// All module mutations share a user/key namespace. The IMMEDIATE transaction
// serializes cross-module claims; legacy schemas may not have every table yet.
func rejectOtherKey(ctx context.Context, tx *sql.Tx, user, key, ownTable string) error {
	for _, table := range []string{"idempotency_keys", "packlist_requests", "catalog_requests", "device_requests", "device_control_requests", "document_requests", "pack_device_check_requests", "device_import_requests", "stock_import_requests", "administrative_requests", "recall_requests", "usage_requests", "document_review_requests", "manual_access_requests", "product_duty_requests", "end_user_acceptance_requests", "reprocessing_requests", "duty_fulfilment_requests", "pack_release_requests"} {
		if table == ownTable {
			continue
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE user_id=? AND key=?)`, user, key).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return conflict("idempotency_conflict", "Dieser Schlüssel gehört zu einem anderen Vorgang.")
		}
	}
	return nil
}
