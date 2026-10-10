import { useMemo, useState } from "react";
import { Check, Download, X } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { Button, Card, ErrorBox, ICON, Loading, Page, PageHeader, Pill, fmtDateTime, relTime, useLoad } from "../../ui/index.ts";
import { approveListing, getListingReview, rejectListing, type ReviewContent, type ReviewMedia } from "./api.ts";
import { ListingFacts, RejectDialog, reviewError, useDecide } from "./ListingReviews.tsx";
import "./admin.css";

/**
 * One listing in full, for a reviewer looking for offensive content:
 * the listing, the organisation's page, the adventure's presentation
 * and every text and picture, sound, film and file of the adventure
 * version it plays — raw, as the server gathers it
 * (internal/admin/listing_content.go). Texts are shown as written, never
 * rendered, so nothing hides in formatting.
 */
export function ListingReviewPage({ kind, id }: { kind: "event" | "party"; id: number }) {
  const t = useT();
  const load = useLoad(async (s) => {
    try {
      return await getListingReview(kind, id, s);
    } catch (e) {
      throw new Error(reviewError(e, t));
    }
  }, [kind, id]);
  const back = <a href={href({ name: "listing-reviews" })}>{t.t("← Listing reviews")}</a>;
  if (load.error && !load.data) {
    return (
      <Page>
        {back}
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      </Page>
    );
  }
  if (!load.data) return <Page><Loading label={t.t("Loading…")} /></Page>;
  return <Review c={load.data} back={back} reload={load.reload} />;
}

function Review({ c, back, reload }: { c: ReviewContent; back: React.ReactNode; reload: () => void }) {
  const t = useT();
  const loc = t.current();
  const [rejecting, setRejecting] = useState(false);
  const [filter, setFilter] = useState("");
  const { busy, decide } = useDecide(reload);
  const pending = c.state === "pending";
  const ref = { kind: c.kind, id: c.id, revision: c.revision };
  const after = () => navigate({ name: "listing-reviews" });

  const texts = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return q ? c.texts.filter((x) => x.text.toLowerCase().includes(q) || x.where.toLowerCase().includes(q)) : c.texts;
  }, [c.texts, filter]);

  const newVersion = !!c.live_graph_id && c.live_graph_id !== c.graph_id;
  const title = c.draft.name || c.adventure_title;
  return (
    <Page>
      {back}
      <PageHeader
        title={
          <span className="ad-inline">
            {title}
            <Pill tone="info">{c.kind === "event" ? t.t("Self-guided") : t.t("Party")}</Pill>
            <StatePill state={c.state} />
          </span>
        }
        actions={
          pending && (
            <span className="ad-actions">
              <Button variant="danger" icon={<X size={ICON.sm} aria-hidden />} disabled={busy} onClick={() => setRejecting(true)}>{t.t("Reject")}</Button>
              <Button
                variant="primary"
                icon={<Check size={ICON.sm} aria-hidden />}
                loading={busy}
                onClick={() => void decide(async () => { await approveListing(ref); after(); }, t.t("Approved: it is public now"))}
              >
                {t.t("Approve")}
              </Button>
            </span>
          )
        }
      />

      <Card title={t.t("About this listing")}>
        <dl className="ad-review__head">
          <dt>{t.t("Organisation")}</dt>
          <dd className="ad-inline">
            <a href={href({ name: "organization", id: c.org_id })}>{c.org_name}</a>
            {c.org_verified && <Pill tone="good">{t.t("Verified creator")}</Pill>}
          </dd>
          <dt>{t.t("Adventure")}</dt>
          <dd className="ad-inline">
            {c.adventure_title} · {t.t("version {{n}}", { n: c.version })}
            {newVersion && <Pill tone="warn">{t.t("A different version from the one approved before")}</Pill>}
          </dd>
          {c.submitted_at && (
            <>
              <dt>{t.t("Submitted")}</dt>
              <dd title={fmtDateTime(c.submitted_at, loc)}>{relTime(c.submitted_at, loc)}</dd>
            </>
          )}
          <dt>{t.t("Content confirmed by")}</dt>
          <dd>
            {c.attested_by
              ? t.t("{{name}}, {{when}}", { name: c.attested_by, when: c.attested_at ? fmtDateTime(c.attested_at, loc) : "" })
              : t.t("(no one)")}
          </dd>
          {c.reject_reason && (
            <>
              <dt>{t.t("Rejected because")}</dt>
              <dd className="ad-texts__text">{c.reject_reason}</dd>
            </>
          )}
        </dl>
      </Card>

      <Card title={t.t("The listing")} subtitle={c.live ? t.t("Marked: what changed since the approved version.") : undefined}>
        <ListingFacts kind={c.kind} draft={c.draft} live={c.live} />
      </Card>

      <Card
        title={t.t("Pictures, sounds, films and files ({{n}})", { n: c.media.length })}
        subtitle={t.t("Everything a player or buyer can see or hear.")}
        flush
      >
        {c.media.length === 0 ? (
          <p className="ad-sub ad-pad">{t.t("None.")}</p>
        ) : (
          <div className="ad-media">
            {c.media.map((m) => <MediaItem key={m.ref} m={m} />)}
          </div>
        )}
      </Card>

      <Card
        title={t.t("Every text ({{n}})", { n: c.texts.length })}
        subtitle={t.t("As written, with where each one is: titles, puzzles, hints, answers, emails, roles, translations.")}
        actions={
          <input
            className="ad-filter"
            type="search"
            placeholder={t.t("Find in the texts")}
            aria-label={t.t("Find in the texts")}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        }
        flush
      >
        {texts.length === 0 ? (
          <p className="ad-sub ad-pad">{filter ? t.t("No text matches.") : t.t("None.")}</p>
        ) : (
          <ul className="ad-texts">
            {texts.map((x, i) => (
              <li key={i}>
                <span className="ad-texts__where">{x.where}</span>
                <span className="ad-texts__text">{x.text}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>

      {rejecting && (
        <RejectDialog
          onClose={() => setRejecting(false)}
          onReject={(reason) => {
            setRejecting(false);
            void decide(async () => { await rejectListing(ref, reason); after(); }, t.t("Rejected: the organisation has been told why"));
          }}
        />
      )}
    </Page>
  );
}

function StatePill({ state }: { state: ReviewContent["state"] }) {
  const t = useT();
  switch (state) {
    case "pending":
      return <Pill tone="warn">{t.t("Waiting for review")}</Pill>;
    case "approved":
      return <Pill tone="good">{t.t("Approved")}</Pill>;
    case "rejected":
      return <Pill tone="bad">{t.t("Rejected")}</Pill>;
    case "draft":
      return <Pill>{t.t("Changed since, not submitted")}</Pill>;
    default:
      return null;
  }
}

/** One media item, shown inline: a file is offered for download. */
function MediaItem({ m }: { m: ReviewMedia }) {
  const t = useT();
  const caption = (
    <figcaption>
      {m.where}
      {m.size > 0 && <> · {formatSize(m.size)}</>}
      {m.mime && <> · {m.mime}</>}
    </figcaption>
  );
  switch (m.kind) {
    case "image":
      return (
        <figure>
          <a href={m.url} target="_blank" rel="noopener"><img src={m.url} alt={m.where} loading="lazy" /></a>
          {caption}
        </figure>
      );
    case "audio":
      return <figure><audio controls preload="none" src={m.url} />{caption}</figure>;
    case "video":
      return <figure><video controls preload="metadata" src={m.url} />{caption}</figure>;
    default:
      return (
        <figure>
          <a className="btn sm" href={m.url} download>
            <Download size={ICON.sm} aria-hidden /> {t.t("Download to check")}
          </a>
          {caption}
        </figure>
      );
  }
}

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} kB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
