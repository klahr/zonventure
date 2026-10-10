import { useT } from "../i18n.tsx";
import { Callout } from "../designer/dz/ui.tsx";

/**
 * Where a store listing or party offer stands in listing review
 * (internal/admin/listing_review.go): review is where its last saved
 * changes are, listed whether an approved version is public.
 */
export function ListingStatus({ review, reason, listed, kind }: { review?: string | undefined; reason?: string | undefined; listed?: boolean | undefined; kind: "store" | "page" }) {
  const t = useT();
  switch (review) {
    case "pending":
      return (
        <Callout variant="info">
          {listed
            ? kind === "store"
              ? t.t("In review: the store shows the approved version until your changes are approved.")
              : t.t("In review: your page shows the approved version until your changes are approved.")
            : kind === "store"
              ? t.t("In review: not in the store yet. It goes up once it is approved.")
              : t.t("In review: not on your page yet. It goes up once it is approved.")}
        </Callout>
      );
    case "rejected":
      return (
        <Callout variant="danger">
          <strong>{t.t("Not approved:")}</strong> {reason}
          <br />
          {listed ? t.t("The approved version stays up. Change it and save to send it for review again.") : t.t("Change it and save to send it for review again.")}
        </Callout>
      );
    case "approved":
      return listed ? <Callout variant="success">{kind === "store" ? t.t("Live in the store.") : t.t("Live on your page.")}</Callout> : null;
    default:
      return null;
  }
}
