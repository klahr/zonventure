import { useState } from "react";
import { ApiError, post } from "@zonventure/api";
import { useT } from "../i18n.tsx";
import { Button, Callout } from "../designer/dz/ui.tsx";
import { Dialog } from "./dialogs.tsx";
import { ErrorBox } from "./kit.tsx";
import { useToast } from "./toast.tsx";
import "./listingStatus.css";

/** Where a listing's draft stands in listing review (internal/admin/listing_review.go). */
export type ListingReviewState = "" | "draft" | "pending" | "approved" | "rejected";

/**
 * Where a store listing or party offer stands in listing review: review
 * is its draft's state, listed whether an approved version is public.
 * Changes are a draft until they are submitted for review.
 * newerVersion: a party offer whose adventure has a newer published
 * version than the approved one, which booked parties keep playing.
 */
export function ListingStatus({ review, reason, listed, kind, newerVersion }: {
  review?: ListingReviewState | undefined;
  reason?: string | undefined;
  listed?: boolean | undefined;
  kind: "store" | "page";
  newerVersion?: boolean | undefined;
}) {
  const t = useT();
  const store = kind === "store";
  switch (review) {
    case "draft":
      return (
        <Callout variant="warning">
          {listed
            ? store
              ? t.t("Not submitted: the store shows the approved version until you submit your changes and they are approved.")
              : t.t("Not submitted: your page shows the approved version until you submit your changes and they are approved.")
            : store
              ? t.t("Not submitted yet: submit it for review to put it in the store.")
              : t.t("Not submitted yet: submit it for review to put it on your page.")}
        </Callout>
      );
    case "pending":
      return (
        <Callout variant="info">
          {listed
            ? store
              ? t.t("In review: the store shows the approved version until your changes are approved.")
              : t.t("In review: your page shows the approved version until your changes are approved.")
            : store
              ? t.t("In review: not in the store yet. It goes up once it is approved.")
              : t.t("In review: not on your page yet. It goes up once it is approved.")}
        </Callout>
      );
    case "rejected":
      return (
        <Callout variant="danger">
          <strong>{t.t("Not approved:")}</strong> {reason}
          <br />
          {listed ? t.t("The approved version stays up. Change it, then submit it again.") : t.t("Change it, then submit it again.")}
        </Callout>
      );
    case "approved":
      if (!listed) return null;
      return (
        <Callout variant={newerVersion ? "warning" : "success"}>
          {store ? t.t("Live in the store.") : t.t("Live on your page.")}
          {newerVersion && (
            <>
              {" "}
              {t.t("The adventure has a newer published version; booked party adventures play the approved one. Submit the offer to move them to the newest version.")}
            </>
          )}
        </Callout>
      );
    default:
      return null;
  }
}

/** Whether a listing has something to submit for review. */
export function canSubmitListing(review?: ListingReviewState | undefined, newerVersion?: boolean | undefined): boolean {
  return review === "draft" || (review === "approved" && !!newerVersion);
}

/**
 * Submitting a listing for review, with the creator's word that its
 * content follows the terms. url is the submit endpoint; done gets the
 * state it landed in: pending, or approved for a verified creator.
 */
export function SubmitListingDialog({ url, title, note, attestText, onClose, onDone }: {
  url: string;
  title?: string | undefined;
  /** What a reviewer checks, when it isn't a listing. */
  note?: string | undefined;
  /** The creator's word, when it isn't for a listing. */
  attestText?: string | undefined;
  onClose: () => void;
  onDone: (state: string) => void;
}) {
  const t = useT();
  const toast = useToast();
  const [attest, setAttest] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit() {
    if (!attest || busy) return;
    setBusy(true);
    setError(null);
    try {
      const res = await post<{ listing_review: string }>(url, { attest: true });
      toast.ok(res.listing_review === "approved"
        ? t.t("Approved: it is public now.")
        : t.t("Submitted. It goes public once it is approved."));
      onDone(res.listing_review);
      onClose();
    } catch (e) {
      setBusy(false);
      if (e instanceof ApiError) {
        switch (e.code) {
          case "unchanged":
            return setError(t.t("Nothing has changed since it was approved."));
          case "not_for_sale":
            return setError(t.t("Set a price, or make the offer bookable, before submitting it."));
          case "unchanged_since_rejected":
            return setError(t.t("Change what was rejected first, then submit it again."));
          case "attest_required":
            return setError(t.t("Confirm the content follows the terms to submit it."));
        }
        return setError(e.message);
      }
      setError(String(e));
    }
  }

  return (
    <Dialog
      title={t.t("Submit for review")}
      onClose={() => !busy && onClose()}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!attest}>{t.t("Submit for review")}</Button>
        </>
      }
    >
      {title && <p><strong>{title}</strong></p>}
      <p className="ls-note">
        {note ?? t.t("A reviewer checks the listing, the whole adventure it plays and your organisation's page before it goes public. What is public now stays up meanwhile.")}
      </p>
      <label className="ls-attest">
        <input type="checkbox" checked={attest} onChange={(e) => setAttest(e.target.checked)} />
        <span>
          {attestText ?? t.t("I confirm this listing and the adventure it plays contain nothing offensive, hateful, sexual, violent or otherwise illegal or unsuitable, and follow the terms.")}{" "}
          <a href="/terms" target="_blank" rel="noopener">{t.t("Read the terms")}</a>
        </span>
      </label>
      {error && <ErrorBox message={error} />}
    </Dialog>
  );
}
