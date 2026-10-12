package retention

import (
	"context"
	"database/sql"
)

// clearPostedAddresses blanks the postal addresses of letters posted
// (or of box redemptions turned down) more than days ago. A postal
// address is personal data needed only to post the letter. The order
// stays, with its email and dates, for bookkeeping and refunds.
func clearPostedAddresses(ctx context.Context, db *sql.DB, days int) {
	runBoundedDelete(ctx, db, "retention.redemption_addresses", `
		UPDATE partner_redemptions SET name = '', street_address = '', address_2 = '', postal_code = '', city = ''
		WHERE id IN (
		    SELECT id FROM partner_redemptions
		    WHERE (street_address <> '' OR name <> '')
		      AND COALESCE(
		            CASE WHEN posted_at IS NOT NULL THEN posted_at END,
		            CASE WHEN state IN ('rejected', 'cancelled') THEN decided_at END
		          )::timestamp AT TIME ZONE 'UTC' < now() - make_interval(days => $1)
		    LIMIT 5000)`, days)
	runBoundedDelete(ctx, db, "retention.purchase_addresses", `
		UPDATE ticket_purchases SET ship_name = '', street_address = '', address_2 = '', postal_code = '', city = ''
		WHERE id IN (
		    SELECT id FROM ticket_purchases
		    WHERE (street_address <> '' OR ship_name <> '')
		      AND COALESCE(
		            CASE WHEN posted_at IS NOT NULL THEN posted_at END,
		            CASE WHEN state = 'refunded' THEN refunded_at END
		          )::timestamp AT TIME ZONE 'UTC' < now() - make_interval(days => $1)
		    LIMIT 5000)`, days)
}
