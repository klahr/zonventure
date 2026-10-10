import { useState, type ReactNode } from "react";
import { Check, ExternalLink, X } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Card, Dialog, Empty, ErrorBox, Field, ICON, Loading, Page, PageHeader, Pill, fmtDateTime, fmtMoney, relTime, useLoad, useToast } from "../../ui/index.ts";
import { approveListing, listListingReviews, rejectListing, type ListingFields, type ListingReview } from "./api.ts";
import "./admin.css";

/**
 * What organisations put up for sale, waiting for a platform admin
 * before it goes public (internal/admin/listing_review.go): a
 * self-guided event's store listing or a party offer. Each shows what
 * the public would see; an edit of a live listing marks what changed.
 */
export function ListingReviewsPage() {
  const t = useT();
  const reviews = useLoad((s) => listListingReviews(s), []);
  const list = reviews.data ?? [];
  return (
    <Page>
      <PageHeader
        title={t.t("Listing reviews")}
        subtitle={t.t("Store listings and party offers go public once you approve them. Verified creators skip this.")}
      />
      {reviews.error && !reviews.data ? (
        <ErrorBox message={t.t("Couldn't load listings ({{err}}).", { err: reviews.error })} retry={reviews.reload} retryLabel={t.t("Retry")} />
      ) : !reviews.data ? (
        <Loading label={t.t("Loading…")} />
      ) : list.length === 0 ? (
        <Empty title={t.t("Nothing is waiting for review.")} />
      ) : (
        list.map((r) => <ReviewCard key={`${r.kind}-${r.id}-${r.revision}`} review={r} onDone={reviews.reload} />)
      )}
    </Page>
  );
}

function ReviewCard({ review: r, onDone }: { review: ListingReview; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [rejecting, setRejecting] = useState(false);
  const d = r.draft;
  const live = r.live;
  const changed = (k: keyof ListingFields) => !!live && (live[k] ?? "") !== (d[k] ?? "");
  const money = (cents: number) => fmtMoney(cents, d.currency || "SEK", t.current());

  // A decision on a listing that changed (or was decided) meanwhile is
  // refused: say so and show the queue as it is now.
  const decide = async (run: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await run();
      toast.ok(done);
      onDone();
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        toast.err(e.code === "changed"
          ? t.t("The listing changed while you looked. Review it again.")
          : t.t("Someone else has already decided this listing."));
        onDone();
      } else {
        toast.err(e);
      }
    } finally {
      setBusy(false);
    }
  };

  const orgLink = href({ name: "organization", id: r.org_id });
  const publicLink = r.org_slug ? `/o/${encodeURIComponent(r.org_slug)}` : null;
  return (
    <Card
      title={
        <span className="ad-inline">
          {d.name || r.adventure_title}
          <Pill tone="info">{r.kind === "event" ? t.t("Self-guided") : t.t("Party")}</Pill>
          <Pill tone={live ? "warn" : "accent"}>{live ? t.t("Edit of a live listing") : t.t("New listing")}</Pill>
        </span>
      }
      subtitle={
        <>
          <a href={orgLink}>{r.org_name}</a>
          {" · "}
          {r.adventure_title}
          {" · "}
          <span title={fmtDateTime(r.submitted_at, t.current())}>{t.t("submitted {{when}}", { when: relTime(r.submitted_at, t.current()) })}</span>
        </>
      }
      actions={
        <span className="ad-actions">
          {publicLink && (
            <a className="ad-inline" href={publicLink} target="_blank" rel="noopener">
              {t.t("Public page")} <ExternalLink size={ICON.sm} aria-hidden />
            </a>
          )}
          <Button size="sm" variant="danger" icon={<X size={ICON.sm} aria-hidden />} disabled={busy} onClick={() => setRejecting(true)}>{t.t("Reject")}</Button>
          <Button size="sm" variant="primary" icon={<Check size={ICON.sm} aria-hidden />} loading={busy} onClick={() => void decide(() => approveListing(r), t.t("Approved: it is public now"))}>{t.t("Approve")}</Button>
        </span>
      }
    >
      <div className="ad-listing">
        {d.cover_url ? (
          <img className={"ad-listing__cover" + (changed("cover_url") ? " is-changed" : "")} src={d.cover_url} alt={t.t("Cover image")} />
        ) : live?.cover_url ? (
          <p className="ad-listing__cover ad-listing__none is-changed">{t.t("Cover removed")}</p>
        ) : null}
        <dl className="ad-listing__facts">
          {d.name !== undefined && r.kind === "event" && (
            <Fact label={t.t("Name")} changed={changed("name")} was={live?.name}>{d.name || <span className="ad-sub">{t.t("(none)")}</span>}</Fact>
          )}
          <Fact label={t.t("Blurb")} changed={changed("blurb")} was={live?.blurb}>{d.blurb || <span className="ad-sub">{t.t("(none)")}</span>}</Fact>
          <Fact label={r.kind === "event" ? t.t("Price per ticket") : t.t("Base price")} changed={changed("price_cents")} was={live ? money(live.price_cents) : undefined}>{money(d.price_cents)}</Fact>
          {r.kind === "party" && (
            <Fact label={t.t("Price per child")} changed={changed("per_kid_cents")} was={live ? money(live.per_kid_cents ?? 0) : undefined}>{money(d.per_kid_cents ?? 0)}</Fact>
          )}
          {r.kind === "event" ? (
            <Fact label={t.t("Players per ticket")} changed={changed("team_size")} was={live?.team_size}>{d.team_size}</Fact>
          ) : (
            <Fact label={t.t("Most children")} changed={changed("max_kids")} was={live?.max_kids}>{d.max_kids}</Fact>
          )}
        </dl>
      </div>
      {rejecting && (
        <RejectDialog
          onClose={() => setRejecting(false)}
          onReject={(reason) => {
            setRejecting(false);
            void decide(() => rejectListing(r, reason), t.t("Rejected: the organisation has been told why"));
          }}
        />
      )}
    </Card>
  );
}

/** One field of the listing; an edited one says what it was. */
function Fact({ label, changed, was, children }: { label: string; changed: boolean; was?: ReactNode; children: ReactNode }) {
  const t = useT();
  return (
    <div className={"ad-listing__fact" + (changed ? " is-changed" : "")}>
      <dt>
        {label}
        {changed && <Pill tone="warn">{t.t("Changed")}</Pill>}
      </dt>
      <dd>
        {children}
        {changed && was !== undefined && was !== "" && <span className="ad-sub ad-listing__was">{t.t("Was: {{v}}", { v: String(was) })}</span>}
      </dd>
    </div>
  );
}

function RejectDialog({ onClose, onReject }: { onClose: () => void; onReject: (reason: string) => void }) {
  const t = useT();
  const [reason, setReason] = useState("");
  const ok = reason.trim() !== "";
  return (
    <Dialog
      title={t.t("Reject this listing?")}
      onClose={onClose}
      onSubmit={() => ok && onReject(reason.trim())}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="danger" filled disabled={!ok}>{t.t("Reject")}</Button>
        </>
      }
    >
      <Field label={t.t("Why? The organisation's admins get this by email.")}>
        <textarea autoFocus rows={4} maxLength={1000} value={reason} onChange={(e) => setReason(e.target.value)} />
      </Field>
      <p className="ad-sub">{t.t("Whatever of theirs is public now stays up.")}</p>
    </Dialog>
  );
}
