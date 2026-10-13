import { useMemo, useState } from "react";
import { Check, Download, X } from "lucide-react";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { Button, Card, ErrorBox, ICON, Loading, Page, PageHeader, Pill, fmtDateTime, relTime, useLoad } from "../../ui/index.ts";
import { getListingReview, type ListingKind, type ReviewChange, type ReviewContent, type ReviewMedia } from "./api.ts";
import { ListingFacts, RejectDialog, isProposal, kindLabel, reviewError, useDecide } from "./ListingReviews.tsx";
import { changedTexts, isPlayerText, textLabel } from "./reviewTexts.ts";
import "./admin.css";

/**
 * One listing in full, for a reviewer looking for offensive content:
 * the listing, the organisation's page, the adventure's presentation
 * and every text and picture, sound, film and file of the adventure
 * version it plays — raw, as the server gathers it
 * (internal/admin/listing_content.go). Texts are shown as written, never
 * rendered, so nothing hides in formatting.
 */
export function ListingReviewPage({ kind, id }: { kind: ListingKind; id: number }) {
  const t = useT();
  const load = useLoad(async (s) => {
    try {
      return await getListingReview(kind, id, s);
    } catch (e) {
      throw new Error(reviewError(e, t));
    }
  }, [kind, id]);
  const back = <a href={href({ name: "listing-reviews" })}>{t.t("← Content review")}</a>;
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
  // The copy the reviewer decided on, kept when it changed under them, to show what changed.
  const [reviewed, setReviewed] = useState<ReviewContent | null>(null);
  const { busy, approve, reject } = useDecide(reload, () => setReviewed(c));
  const { state } = useSession();
  // Support reads the review but never decides.
  const decides = state.status === "authenticated" && !!state.user.listing_reviewer && state.user.role !== "support";
  const pending = c.state === "pending";
  const ref = { kind: c.kind, id: c.id, revision: c.revision };
  const after = () => navigate({ name: "listing-reviews" });

  const label = (where: string) => textLabel(where, c.step_titles, t);
  const all = useMemo(() => c.texts.filter((x) => isPlayerText(x.where)), [c.texts]);
  const texts = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return q ? all.filter((x) => x.text.toLowerCase().includes(q) || textLabel(x.where, c.step_titles, t).toLowerCase().includes(q)) : all;
  }, [all, filter, c.step_titles, t]);
  const changedNow = reviewed && reviewed.revision !== c.revision ? changedTexts(reviewed.texts.filter((x) => isPlayerText(x.where)), all) : null;

  const newVersion = !!c.live_graph_id && c.live_graph_id !== c.graph_id;
  const proposal = isProposal(c.kind);
  const title = c.kind === "org" ? c.org_name : c.draft.name || c.adventure_title;
  return (
    <Page>
      {back}
      <PageHeader
        title={
          <span className="adm-inline">
            {title}
            <Pill tone="info">{kindLabel(t, c.kind)}</Pill>
            <StatePill state={c.state} />
          </span>
        }
        actions={
          pending && decides && (
            <span className="adm-actions">
              <Button variant="danger" icon={<X size={ICON.sm} aria-hidden />} disabled={busy} onClick={() => setRejecting(true)}>{t.t("Reject")}</Button>
              <Button
                variant="primary"
                icon={<Check size={ICON.sm} aria-hidden />}
                loading={busy}
                onClick={() => void approve(ref, after)}
              >
                {t.t("Approve")}
              </Button>
            </span>
          )
        }
      />

      <Card title={proposal ? t.t("About this change") : t.t("About this listing")}>
        <dl className="adm-review__head">
          <dt>{t.t("Organisation")}</dt>
          <dd className="adm-inline">
            <a href={href({ name: "organization", id: c.org_id })}>{c.org_name}</a>
            {c.org_verified && <Pill tone="good">{t.t("Verified creator")}</Pill>}
          </dd>
          {c.kind !== "org" && (
            <>
              <dt>{t.t("Adventure")}</dt>
              <dd className="adm-inline">
                {c.adventure_title}
                {!proposal && <> · {t.t("version {{n}}", { n: c.version })}</>}
                {newVersion && <Pill tone="warn">{t.t("A different version from the one approved before")}</Pill>}
              </dd>
            </>
          )}
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
              <dd className="adm-texts__text">{c.reject_reason}</dd>
            </>
          )}
        </dl>
      </Card>

      {reviewed && changedNow && (
        <Card title={t.t("Changed while you looked")} subtitle={t.t("What changed since you opened it. Nothing was decided: look it over and decide again.")}>
          {!proposal && <ListingFacts kind={c.kind} draft={c.draft} live={reviewed.draft} />}
          {changedNow.length > 0 && (
            <ul className="adm-texts">
              {changedNow.map((x) => (
                <li key={x.where}>
                  <span className="adm-texts__where">{label(x.where)}</span>
                  <span className="adm-texts__text">
                    {x.text || <span className="adm-sub">{t.t("(removed)")}</span>}
                    {x.was && <span className="adm-sub adm-listing__was">{t.t("Was: {{v}}", { v: x.was })}</span>}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}

      {proposal ? (
        <Card title={t.t("What changes")} subtitle={t.t("Each field as it is public now, and as proposed. Below: the page as it would be.")}>
          <Changes changes={c.changes ?? []} />
        </Card>
      ) : (
        <Card title={t.t("The listing")} subtitle={c.live ? t.t("Marked: what changed since the approved version.") : undefined}>
          <ListingFacts kind={c.kind} draft={c.draft} live={c.live} />
        </Card>
      )}

      <Card
        title={t.t("Pictures, sounds, films and files ({{n}})", { n: c.media.length })}
        subtitle={t.t("Everything a player or buyer can see or hear.")}
        flush
      >
        {c.media.length === 0 ? (
          <p className="adm-sub adm-pad">{t.t("None.")}</p>
        ) : (
          <div className="adm-media">
            {c.media.map((m) => <MediaItem key={m.ref} m={m} />)}
          </div>
        )}
      </Card>

      <Card
        title={t.t("Every text ({{n}})", { n: all.length })}
        subtitle={t.t("As written, with where each one is: titles, puzzles, hints, answers, emails, roles, translations.")}
        actions={
          <input
            className="adm-filter"
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
          <p className="adm-sub adm-pad">{filter ? t.t("No text matches.") : t.t("None.")}</p>
        ) : (
          <ul className="adm-texts">
            {texts.map((x, i) => (
              <li key={i}>
                <span className="adm-texts__where" title={x.where}>{label(x.where)}</span>
                <span className="adm-texts__text">{x.text}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>

      {rejecting && (
        <RejectDialog
          proposal={proposal}
          onClose={() => setRejecting(false)}
          onReject={(reason) => {
            setRejecting(false);
            void reject(ref, reason, after);
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

/** What a proposal changes, field by field: public now, and proposed. */
function Changes({ changes }: { changes: ReviewChange[] }) {
  const t = useT();
  if (changes.length === 0) return <p className="adm-sub">{t.t("Nothing.")}</p>;
  const side = (text: string | undefined, url: string | undefined, empty: string) =>
    url ? <img className="adm-change__img" src={url} alt="" /> : text ? <span className="adm-texts__text">{text}</span> : <span className="adm-sub">{empty}</span>;
  return (
    <table className="adm-changes">
      <thead>
        <tr>
          <th scope="col">{t.t("Field")}</th>
          <th scope="col">{t.t("Public now")}</th>
          <th scope="col">{t.t("Proposed")}</th>
        </tr>
      </thead>
      <tbody>
        {changes.map((ch) => (
          <tr key={ch.field}>
            <th scope="row">{ch.field}</th>
            <td>{side(ch.live, ch.live_url, t.t("(empty)"))}</td>
            <td>{side(ch.proposed, ch.proposed_url, t.t("(empty)"))}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

