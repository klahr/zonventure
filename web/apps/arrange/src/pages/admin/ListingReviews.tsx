import { useState, type ReactNode } from "react";
import { Bot, Check, ExternalLink, FileSearch, Trash2, UserPlus, X, Zap } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Card, Dialog, Empty, ErrorBox, Field, ICON, Loading, Page, PageHeader, Pill, Table, Tabs, fmtDateTime, fmtMoney, relTime, useLoad, useToast, type Column } from "../../ui/index.ts";
import {
  approveListing, listListingLog, listListingReviewers, listListingReviews, rejectListing, setListingReviewer, verifyListingLog,
  type ChainReport, type ListingFields, type ListingLogEntry, type ListingRef, type ListingReview, type ListingReviewer,
} from "./api.ts";
import "./admin.css";

type Translator = ReturnType<typeof useT>;

/**
 * What organisations put up for sale, waiting for a listing reviewer
 * before it goes public (internal/admin/listing_review.go): a
 * self-guided event's store listing or a party offer. The queue shows
 * each listing as the public would; its full page shows everything it
 * puts in front of people. Reviewers are chosen by platform admins, and
 * never see their own organisations' listings. The log is for
 * spot-checking what AI assistants and verified creators let through.
 */
export function ListingReviewsPage({ tab, reviewer, admin }: { tab?: string | undefined; reviewer: boolean; admin: boolean }) {
  const t = useT();
  const current = tab === "log" || (tab === "reviewers" && admin) ? tab : "queue";
  return (
    <Page>
      <PageHeader
        title={t.t("Listing reviews")}
        subtitle={t.t("Store listings and party offers go public once a reviewer approves them. Verified creators skip this.")}
      />
      <Tabs
        label={t.t("Listing reviews")}
        current={current}
        tabs={[
          { id: "queue", label: t.t("Waiting"), href: href({ name: "listing-reviews" }) },
          { id: "log", label: t.t("Recent decisions"), href: href({ name: "listing-reviews", tab: "log" }) },
          { id: "reviewers", label: t.t("Reviewers"), href: href({ name: "listing-reviews", tab: "reviewers" }), hidden: !admin },
        ]}
      />
      {current === "queue" && (reviewer ? <Queue /> : <NotReviewer />)}
      {current === "log" && (reviewer ? <Log /> : <NotReviewer />)}
      {current === "reviewers" && <Reviewers />}
    </Page>
  );
}

function NotReviewer() {
  const t = useT();
  return (
    <Empty title={t.t("You aren't a listing reviewer.")}>
      {t.t("A platform admin chooses who reviews listings, under Reviewers.")}
    </Empty>
  );
}

/** The server's refusals in the reader's words. */
export function reviewError(e: unknown, t: Translator): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case "not_reviewer":
        return t.t("You aren't a listing reviewer.");
      case "own_org":
        return t.t("This is your own organisation's listing: someone outside it has to review it.");
      case "changed":
        return t.t("The listing changed while you looked. Review it again.");
      case "not_pending":
        return t.t("This listing isn't waiting for review any more: someone has decided it, or it was changed.");
    }
    return e.message;
  }
  return e instanceof Error ? e.message : String(e);
}

/** Runs a decision; a refused one is said in words and the list reloaded. */
export function useDecide(onDone: () => void) {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const decide = async (run: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await run();
      toast.ok(done);
      onDone();
    } catch (e) {
      toast.err(reviewError(e, t));
      if (e instanceof ApiError && e.status === 409) onDone();
    } finally {
      setBusy(false);
    }
  };
  return { busy, decide };
}

function Queue() {
  const t = useT();
  const reviews = useLoad((s) => listListingReviews(s), []);
  const list = reviews.data ?? [];
  if (reviews.error && !reviews.data) {
    return <ErrorBox message={t.t("Couldn't load listings ({{err}}).", { err: reviews.error })} retry={reviews.reload} retryLabel={t.t("Retry")} />;
  }
  if (!reviews.data) return <Loading label={t.t("Loading…")} />;
  if (list.length === 0) return <Empty title={t.t("Nothing is waiting for review.")} />;
  return <>{list.map((r) => <ReviewCard key={`${r.kind}-${r.id}-${r.revision}`} review={r} onDone={reviews.reload} />)}</>;
}

function ReviewCard({ review: r, onDone }: { review: ListingReview; onDone: () => void }) {
  const t = useT();
  const [rejecting, setRejecting] = useState(false);
  const { busy, decide } = useDecide(onDone);
  const d = r.draft;
  const live = r.live;
  const ref: ListingRef = r;

  const orgLink = href({ name: "organization", id: r.org_id });
  const publicLink = r.org_slug ? `/o/${encodeURIComponent(r.org_slug)}` : null;
  return (
    <Card
      title={
        <span className="ad-inline">
          {d.name || r.adventure_title}
          <Pill tone="info">{r.kind === "event" ? t.t("Self-guided") : t.t("Party")}</Pill>
          <Pill tone={live ? "warn" : "accent"}>{live ? t.t("Edit of a live listing") : t.t("New listing")}</Pill>
          {live && live.graph_id !== d.graph_id && <Pill tone="warn">{t.t("New adventure version")}</Pill>}
        </span>
      }
      subtitle={
        <>
          <a href={orgLink}>{r.org_name}</a>
          {" · "}
          {r.adventure_title}
          {" · "}
          <span title={fmtDateTime(r.submitted_at, t.current())}>{t.t("submitted {{when}}", { when: relTime(r.submitted_at, t.current()) })}</span>
          {r.attested_by && <>{" · "}{t.t("by {{name}}", { name: r.attested_by })}</>}
        </>
      }
      actions={
        <span className="ad-actions">
          {publicLink && (
            <a className="ad-inline" href={publicLink} target="_blank" rel="noopener">
              {t.t("Public page")} <ExternalLink size={ICON.sm} aria-hidden />
            </a>
          )}
          <a className="btn sm" href={href({ name: "listing-review", kind: r.kind, id: r.id })}>
            <FileSearch size={ICON.sm} aria-hidden /> {t.t("Review in full")}
          </a>
          <Button size="sm" variant="danger" icon={<X size={ICON.sm} aria-hidden />} disabled={busy} onClick={() => setRejecting(true)}>{t.t("Reject")}</Button>
          <Button size="sm" variant="primary" icon={<Check size={ICON.sm} aria-hidden />} loading={busy} onClick={() => void decide(() => approveListing(ref), t.t("Approved: it is public now"))}>{t.t("Approve")}</Button>
        </span>
      }
    >
      <ListingFacts kind={r.kind} draft={d} live={live} />
      <p className="ad-sub">{t.t("Approve only once you have looked at all of it: open “Review in full” for every text and picture of the adventure.")}</p>
      {rejecting && (
        <RejectDialog
          onClose={() => setRejecting(false)}
          onReject={(reason) => {
            setRejecting(false);
            void decide(() => rejectListing(ref, reason), t.t("Rejected: the organisation has been told why"));
          }}
        />
      )}
    </Card>
  );
}

/** The listing as the public sees it, with what an edit changed. */
export function ListingFacts({ kind, draft: d, live }: { kind: "event" | "party"; draft: ListingFields; live?: ListingFields | undefined }) {
  const t = useT();
  const changed = (k: keyof ListingFields) => !!live && (live[k] ?? "") !== (d[k] ?? "");
  const money = (cents: number) => fmtMoney(cents, d.currency || "SEK", t.current());
  return (
    <div className="ad-listing">
      {d.cover_url ? (
        <img className={"ad-listing__cover" + (changed("cover_url") ? " is-changed" : "")} src={d.cover_url} alt={t.t("Cover image")} />
      ) : live?.cover_url ? (
        <p className="ad-listing__cover ad-listing__none is-changed">{t.t("Cover removed")}</p>
      ) : null}
      <dl className="ad-listing__facts">
        {kind === "event" && (
          <Fact label={t.t("Name")} changed={changed("name")} was={live?.name}>{d.name || <span className="ad-sub">{t.t("(none)")}</span>}</Fact>
        )}
        <Fact label={t.t("Blurb")} changed={changed("blurb")} was={live?.blurb}>{d.blurb || <span className="ad-sub">{t.t("(none)")}</span>}</Fact>
        <Fact label={kind === "event" ? t.t("Price per ticket") : t.t("Base price")} changed={changed("price_cents")} was={live ? money(live.price_cents) : undefined}>{money(d.price_cents)}</Fact>
        {kind === "party" && (
          <Fact label={t.t("Price per child")} changed={changed("per_kid_cents")} was={live ? money(live.per_kid_cents ?? 0) : undefined}>{money(d.per_kid_cents ?? 0)}</Fact>
        )}
        {kind === "event" ? (
          <Fact label={t.t("Players per ticket")} changed={changed("team_size")} was={live?.team_size}>{d.team_size}</Fact>
        ) : (
          <Fact label={t.t("Most children")} changed={changed("max_kids")} was={live?.max_kids}>{d.max_kids}</Fact>
        )}
      </dl>
    </div>
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

export function RejectDialog({ onClose, onReject }: { onClose: () => void; onReject: (reason: string) => void }) {
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
      <p className="ad-sub">{t.t("Say what to change and where. Whatever of theirs is public now stays up.")}</p>
    </Dialog>
  );
}

/* ---------------- recent decisions ---------------- */

/** Who made a decision: a person in the console, an AI assistant, or no one (a verified creator). */
export function ViaPill({ via }: { via: ListingLogEntry["via"] }) {
  const t = useT();
  if (via === "mcp") return <Pill tone="warn"><Bot size={ICON.sm} aria-hidden /> {t.t("AI assistant")}</Pill>;
  if (via === "auto") return <Pill tone="info"><Zap size={ICON.sm} aria-hidden /> {t.t("Automatic")}</Pill>;
  return <Pill>{t.t("Console")}</Pill>;
}

function Log() {
  const t = useT();
  const loc = t.current();
  const [via, setVia] = useState("");
  const log = useLoad((s) => listListingLog(via, s), [via]);
  const [check, setCheck] = useState<ChainReport | null>(null);
  const [checking, setChecking] = useState(false);
  const [checkError, setCheckError] = useState("");
  const verify = async () => {
    setChecking(true);
    setCheckError("");
    try {
      setCheck(await verifyListingLog());
    } catch (e) {
      setCheckError(e instanceof Error ? e.message : String(e));
    } finally {
      setChecking(false);
    }
  };
  const action = (a: ListingLogEntry["action"]) =>
    a === "approved" ? <Pill tone="good">{t.t("Approved")}</Pill> : a === "rejected" ? <Pill tone="bad">{t.t("Rejected")}</Pill> : <Pill>{t.t("Submitted")}</Pill>;
  const columns: Column<ListingLogEntry>[] = [
    { key: "when", header: t.t("When"), cell: (e) => <span title={fmtDateTime(e.created_at, loc)}>{relTime(e.created_at, loc)}</span> },
    { key: "what", header: t.t("Listing"), cell: (e) => (
      <span className="ad-stack">
        <strong>{e.title || (e.kind === "event" ? t.t("Self-guided") : t.t("Party"))}</strong>
        <span className="ad-sub">{e.org_name}</span>
      </span>
    ) },
    { key: "action", header: t.t("Action"), cell: (e) => action(e.action) },
    { key: "via", header: t.t("By"), cell: (e) => (
      <span className="ad-inline"><ViaPill via={e.via} />{e.by && <span className="ad-sub">{e.by}</span>}</span>
    ) },
    { key: "reason", header: t.t("Reason"), wide: true, cell: (e) => e.reason ? <span className="ad-sub">{e.reason}</span> : null },
  ];
  return (
    <Card
      title={t.t("Recent decisions")}
      subtitle={t.t("Spot-check what AI assistants approved and what verified creators put up without review.")}
      actions={
        <span className="ad-inline">
          <Button onClick={verify} disabled={checking}>{checking ? t.t("Checking…") : t.t("Check the log")}</Button>
          <select aria-label={t.t("Show")} value={via} onChange={(e) => setVia(e.target.value)}>
            <option value="">{t.t("All")}</option>
            <option value="mcp">{t.t("AI assistant (MCP)")}</option>
            <option value="auto">{t.t("Automatic (verified)")}</option>
            <option value="console">{t.t("Console")}</option>
          </select>
        </span>
      }
      flush
    >
      {checkError && <ErrorBox message={checkError} />}
      {check && (
        <div className="ad-pad" role="status">
          {check.ok ? (
            <p><Pill tone="good">{t.t("Intact")}</Pill> {t.t("Every hash of the {{n}} entries adds up. The latest:", { n: check.entries })}</p>
          ) : (
            <>
              <p><Pill tone="bad">{t.t("Changed")}</Pill> {t.t("The log was changed after it was written:")}</p>
              <ul>{check.problems.map((p) => <li key={`${p.id}-${p.problem}`}>#{p.id}: {p.problem}</li>)}</ul>
              <p>{t.t("The latest hash now:")}</p>
            </>
          )}
          <p><code className="ad-hash">{check.head_hash || "—"}</code></p>
          <p className="ad-sub">{t.t("Compare it with the fingerprint mailed to platform admins each day: if one mailed earlier can't be found in the log, it was rewritten.")}</p>
        </div>
      )}
      {log.error && !log.data ? (
        <ErrorBox message={log.error} retry={log.reload} retryLabel={t.t("Retry")} />
      ) : !log.data ? (
        <Loading label={t.t("Loading…")} />
      ) : log.data.length === 0 ? (
        <p className="ad-sub ad-pad">{t.t("Nothing yet.")}</p>
      ) : (
        <Table
          columns={columns}
          rows={log.data}
          rowKey={(e) => `${e.kind}-${e.id}-${e.created_at}-${e.action}-${e.via}`}
          rowHref={(e) => href({ name: "listing-review", kind: e.kind, id: e.id })}
          caption={t.t("Recent decisions")}
        />
      )}
    </Card>
  );
}

/* ---------------- reviewers ---------------- */

/** Platform admins choose who reviews listings, person by person. */
function Reviewers() {
  const t = useT();
  const toast = useToast();
  const list = useLoad((s) => listListingReviewers(s), []);
  const [username, setUsername] = useState("");
  const [busy, setBusy] = useState(false);
  const set = async (name: string, on: boolean) => {
    setBusy(true);
    try {
      await setListingReviewer(name, on);
      toast.ok(on ? t.t("{{name}} reviews listings now", { name }) : t.t("{{name}} no longer reviews listings", { name }));
      if (on) setUsername("");
      list.reload();
    } catch (e) {
      toast.err(e instanceof ApiError && e.status === 404 ? t.t("No user is called {{name}}.", { name }) : e);
    } finally {
      setBusy(false);
    }
  };
  const columns: Column<ListingReviewer>[] = [
    { key: "name", header: t.t("Reviewer"), cell: (r) => (
      <span className="ad-stack">
        <strong className="ad-inline">{r.username}{r.is_admin && <Pill tone="accent">{t.t("Platform admin")}</Pill>}</strong>
        {r.email && <span className="ad-sub">{r.email}</span>}
      </span>
    ) },
    { key: "orgs", header: t.t("Never reviews"), wide: true, cell: (r) => r.orgs.length ? <span className="ad-sub">{r.orgs.join(", ")}</span> : <span className="ad-sub">{t.t("(no organisation)")}</span> },
    { key: "remove", header: <span className="cs-sr">{t.t("Remove")}</span>, align: "end", width: "1%", cell: (r) => (
      <Button size="icon" variant="ghost" disabled={busy} onClick={() => void set(r.username, false)} aria-label={t.t("Remove {{name}} as reviewer", { name: r.username })} title={t.t("Remove as reviewer")}>
        <Trash2 size={ICON.sm} aria-hidden />
      </Button>
    ) },
  ];
  return (
    <Card
      title={t.t("Reviewers")}
      subtitle={t.t("Who may review listings. A reviewer never sees their own organisations' listings. Give an AI assistant an account of its own, make it a reviewer, and its API token can review and nothing else.")}
      flush={(list.data?.length ?? 0) > 0}
    >
      <form className="ad-row ad-pad" onSubmit={(e) => { e.preventDefault(); if (username.trim()) void set(username.trim(), true); }}>
        <Field label={t.t("Username")}>
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
        </Field>
        <Button type="submit" variant="primary" icon={<UserPlus size={ICON.sm} aria-hidden />} loading={busy} disabled={!username.trim()}>{t.t("Make reviewer")}</Button>
      </form>
      {list.error && !list.data ? (
        <ErrorBox message={list.error} retry={list.reload} retryLabel={t.t("Retry")} />
      ) : !list.data ? (
        <Loading label={t.t("Loading…")} />
      ) : list.data.length === 0 ? (
        <p className="ad-sub ad-pad">{t.t("No one reviews listings yet.")}</p>
      ) : (
        <Table columns={columns} rows={list.data} rowKey={(r) => String(r.id)} caption={t.t("Reviewers")} />
      )}
    </Card>
  );
}
