// admin_ops.go — admin-only commerce operations (PKG-2 Wave 4 D7, Q5 GO).
//
// What lives here: cross-product-type admin operations that need the
// commerce-layer abstractions. v0 ships one operation:
//
//   MarkPaid(ctx, db, orderNo, productType, adminUserID)
//     Treats an order as paid — for service products, calls
//     commerce.GrantEntitlement (which CAS-flips gtk_service_order
//     pending_payment → paid). For token plans, the equivalent path is
//     the LS webhook + ProvisionForPlan; admin mark-paid for token plans
//     would require a synthetic LS order id and is out of scope for v0
//     (operators reach LemonSqueezy dashboard directly for that).
//
// Why this lives in commerce, not service/admin:
//   - It dispatches by ProductType, mirroring GrantEntitlement / Revoke.
//   - Future admin operations (admin_revoke, admin_force_close_session)
//     will belong here too.
//
// The HTTP handler is thin — see service/router.go where the route is
// registered. Wave 4 D7 chose service/router.go (rather than admin/router.go)
// because the admin/* routes are CoAI-upstream and we don't want to fork
// that file's import surface; the gtk/v1/admin/mark-paid route is our own
// greentokey namespace.

package commerce

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"chat/globals"
)

// MarkPaid is the unified admin "mark this order as paid" operation. v0
// only supports ProductService — for token plans, admin reaches the LS
// dashboard or grants quota directly. ServiceOnly contract is enforced
// here so the HTTP route can stay branch-free.
//
// Idempotent: GrantEntitlement's CAS observes status='paid' on a
// re-mark and no-ops without error.
//
// The adminUserID is logged for audit (Codex M3 requested a
// created_by_admin_id column on gtk_service_order; that's a follow-up
// schema change — for v0 we just log the actor here so post-incident
// forensics has the trail in stdout/Loki).
func MarkPaid(ctx context.Context, db *sql.DB, orderNo string, productType ProductType, adminUserID int64) error {
	if orderNo == "" {
		return errors.New("commerce.MarkPaid: orderNo required")
	}
	if productType != ProductService {
		return fmt.Errorf(
			"commerce.MarkPaid: only ProductService is supported in v0 (got %q); "+
				"for ProductToken use the LemonSqueezy dashboard",
			productType)
	}

	grant := EntitlementGrant{
		ProductType: ProductService,
		OrderNo:     orderNo,
	}
	if err := GrantEntitlement(ctx, db, grant); err != nil {
		return fmt.Errorf("commerce.MarkPaid: GrantEntitlement: %w", err)
	}

	globals.Info(fmt.Sprintf(
		"commerce.MarkPaid: admin=%d marked order %s as paid (productType=%s)",
		adminUserID, orderNo, productType))
	return nil
}
