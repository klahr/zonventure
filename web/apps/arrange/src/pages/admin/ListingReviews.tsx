import { useState, type ReactNode } from "react";
import { Bot, Check, ExternalLink, FileSearch, KeyRound, Trash2, UserPlus, X, Zap } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href } from "../../router.ts";
import { Button, Callout, Card, Dialog, Empty, ErrorBox, Field, ICON, Loading, Page, PageHeader, Pill, Table, Tabs, fmtDateTime, fmtMoney, relTime, useDialogs, useLoad, useToast, type Column } from "../../ui/index.ts";
import { createPasskey, isCancelled, passkeysSupported, signWithPasskey } from "../../ui/webauthn.ts";
import {
  approveListing, beginReviewerPasskey, finishReviewerPasskey, listListingLog, listListingReviewers, listListingReviews, listReviewerPasskeys,
  rejectListing, revokeReviewerPasskey, setListingReviewer, signListingDecision, verifyListingLog,
  type ChainReport, type ListingKind, type ServedFinding, type ServedReport, type DecisionSignature, type ListingFields, type ReviewerPasskey, type ListingLogEntry, type ListingRef, type ListingReview, type ListingReviewer,
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
/** Every global admin reads the queue and the log; only a reviewer (never Support) decides. */
export function ListingReviewsPage({ tab, reviewer, admin, support }: { tab?: string | undefined; reviewer: boolean; admin: boolean; support: boolean }) {
  const t = useT();
  const current = tab === "log" || (tab === "reviewers" && admin) || (tab === "passkeys" && reviewer && !support) ? tab : "queue";
  return (
    <Page>
      <PageHeader
        title={t.t("Content review")}
        subtitle={t.t("Store listings and party adventures go public once a reviewer approves them. Verified creators skip this.")}
      />
      <Tabs
        label={t.t("Content review")}
        current={current}
        tabs={[
          { id: "queue", label: t.t("Waiting"), href: href({ name: "listing-reviews" }) },
          { id: "log", label: t.t("Recent decisions"), href: href({ name: "listing-reviews", tab: "log" }) },
          { id: "passkeys", label: t.t("Your passkeys"), href: href({ name: "listing-reviews", tab: "passkeys" }), hidden: !reviewer || support },
          { id: "reviewers", label: t.t("Reviewers"), href: href({ name: "listing-reviews", tab: "reviewers" }), hidden: !admin },
        ]}
      />
      {current === "queue" && (reviewer || admin || support ? <Queue readOnly={!reviewer || support} support={support} /> : <NotReviewer admin={admin} />)}
      {current === "log" && (reviewer || admin || support ? <Log /> : <NotReviewer admin={admin} />)}
      {current === "passkeys" && <Passkeys />}
      {current === "reviewers" && <Reviewers />}
    </Page>
  );
}

function NotReviewer({ admin }: { admin: boolean }) {
  const t = useT();
  return (
    <Empty title={t.t("You aren't a listing reviewer.")}>
      {admin
        ? t.t("To review listings yourself, make yourself a reviewer under Reviewers.")
        : t.t("A platform admin chooses who reviews listings, under Reviewers.")}
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
      case "passkey_required":
        return t.t("Add a passkey under “Your passkeys” to sign your decisions with it.");
      case "bad_signature":
        return t.t("Your passkey's signature didn't check out, so nothing was decided. Try again.");
      case "passkeys_unavailable":
        return t.t("Passkeys aren't set up on this server (it needs its public URL), so decisions can't be signed.");
      case "browser_only":
        return t.t("Passkeys work in the browser, not through an API token.");
    }
    return e.message;
  }
  // The browser refuses when this page's address isn't the server's public URL (the passkey's domain).
  if (e instanceof DOMException && e.name === "SecurityError") return t.t("Passkeys work only at this server's public address. Open the console there, or ask the operator to fix the server's public URL.");
  return e instanceof Error ? e.message : String(e);
}

/** A reviewer without a passkey said yes to deciding unsigned, for this browser session. */
const UNSIGNED_OK = "zv-review-unsigned-ok";

function unsignedOK(): boolean {
  try {
    return sessionStorage.getItem(UNSIGNED_OK) === "1";
  } catch {
    return false;
  }
}

function rememberUnsignedOK() {
  try {
    sessionStorage.setItem(UNSIGNED_OK, "1");
  } catch {
    /* storage blocked: asked again next time */
  }
}

/**
 * Makes a decision, signed with the reviewer's passkey when they can:
 * the server turns the decision into a challenge, the passkey signs it,
 * and the decision goes in with the signature
 * (internal/admin/reviewer_passkeys.go). Signing is optional, but going
 * without takes a confirmation — with no passkey, when the passkey is
 * cancelled, or when the server can't do passkeys. Without a passkey it
 * is asked once per browser session, not on every decision. The reason is trimmed
 * once, so the one signed is the one sent. A refused decision is said in
 * words and the list reloaded; onChanged hears when that was because the
 * listing changed while the reviewer looked.
 */
export function useDecide(onDone: () => void, onChanged?: () => void) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [busy, setBusy] = useState(false);
  /** The signature, or why there is none: "none" without a passkey, "cancelled" when the reviewer stopped it. */
  const sign = async (ref: ListingRef, action: "approve" | "reject", reason: string): Promise<DecisionSignature | "none" | "cancelled"> => {
    if (!passkeysSupported() || (await listReviewerPasskeys()).length === 0) return "none";
    try {
      const c = await signListingDecision(ref, action, reason);
      return { session_id: c.session_id, credential: await signWithPasskey(c.options) };
    } catch (e) {
      if (isCancelled(e)) return "cancelled";
      if (e instanceof ApiError && (e.code === "passkey_required" || e.code === "passkeys_unavailable")) return "none";
      throw e;
    }
  };
  const confirmUnsigned = (action: "approve" | "reject", why: "none" | "cancelled") =>
    dialogs.confirm({
      title: action === "approve" ? t.t("Approve without a signature?") : t.t("Reject without a signature?"),
      message: (
        <>
          {(why === "cancelled"
            ? t.t("You didn't sign with your passkey.")
            : t.t("You have no passkey to sign with.")) + " " +
            t.t("The log records the decision under your name, without a passkey signature to prove it. Signing is optional: add a passkey under “Your passkeys” to sign your decisions.")}
          {why === "none" && passkeysSupported() && (
            <>
              {" "}
              <a href={href({ name: "listing-reviews", tab: "passkeys" })} target="_blank" rel="noopener">{t.t("Set up a passkey")}</a>
            </>
          )}
        </>
      ),
      confirmLabel: action === "approve" ? t.t("Approve unsigned") : t.t("Reject unsigned"),
      danger: true,
    });
  const run = async (action: "approve" | "reject", ref: ListingRef, rawReason: string, after?: () => void) => {
    const reason = action === "reject" ? rawReason.trim() : "";
    setBusy(true);
    try {
      const signed = await sign(ref, action, reason);
      const signature = typeof signed === "object" ? signed : undefined;
      if (!signature && !(signed === "none" && unsignedOK()) && !(await confirmUnsigned(action, signed as "none" | "cancelled"))) {
        toast.err(t.t("Nothing was decided."));
        return;
      }
      if (signed === "none") rememberUnsignedOK();
      if (action === "approve") await approveListing(ref, signature);
      else await rejectListing(ref, reason, signature);
      toast.ok(
        (action === "approve" ? t.t("Approved: it is public now.") : t.t("Rejected: the organisation has been told why.")) +
          " " + (signature ? t.t("Signed with your passkey.") : t.t("Not signed.")),
      );
      after?.();
      onDone();
    } catch (e) {
      toast.err(reviewError(e, t));
      if (e instanceof ApiError && e.code === "changed") onChanged?.();
      if (e instanceof ApiError && e.status === 409) onDone();
    } finally {
      setBusy(false);
    }
  };
  return {
    busy,
    approve: (ref: ListingRef, after?: () => void) => run("approve", ref, "", after),
    reject: (ref: ListingRef, reason: string, after?: () => void) => run("reject", ref, reason, after),
  };
}

function Queue({ readOnly = false, support = false }: { readOnly?: boolean; support?: boolean }) {
  const t = useT();
  const reviews = useLoad((s) => listListingReviews(s), []);
  const keys = useLoad((s) => (readOnly ? Promise.resolve(null) : listReviewerPasskeys(s)), [readOnly]);
  // The copy a reviewer decided on, by listing, when it changed under them.
  const [reviewed, setReviewed] = useState<Record<string, ListingReview>>({});
  const needKey = keys.data?.length === 0 && (
    <Card title={t.t("Sign your decisions")} subtitle={t.t("Optional: add a passkey and every decision you make here is signed with it, so no one can make or change one in your name.")}>
      <a className="btn sm" href={href({ name: "listing-reviews", tab: "passkeys" })}>
        <KeyRound size={ICON.sm} aria-hidden /> {t.t("Add a passkey")}
      </a>
    </Card>
  );
  // Why Approve and Reject are missing, and where that changes.
  const readOnlyNote = readOnly && (
    <Callout variant="info">
      {support
        ? t.t("Support reads the queue; only a listing reviewer approves or rejects.")
        : <>{t.t("You can read the queue, but only a listing reviewer approves or rejects.")} <a href={href({ name: "listing-reviews", tab: "reviewers" })}>{t.t("Make yourself a reviewer under Reviewers")}</a></>}
    </Callout>
  );
  const list = reviews.data ?? [];
  if (reviews.error && !reviews.data) {
    return <ErrorBox message={t.t("Couldn't load listings ({{err}}).", { err: reviews.error })} retry={reviews.reload} retryLabel={t.t("Retry")} />;
  }
  if (!reviews.data) return <Loading label={t.t("Loading…")} />;
  if (list.length === 0) return <>{needKey}<Empty title={t.t("Nothing is waiting for review.")} /></>;
  return (
    <>
      {needKey}
      {readOnlyNote}
      {list.map((r) => (
        <ReviewCard
          key={`${r.kind}-${r.id}-${r.revision}`}
          review={r}
          reviewed={reviewed[`${r.kind}-${r.id}`]}
          onDone={reviews.reload}
          onChanged={() => setReviewed((m) => ({ ...m, [`${r.kind}-${r.id}`]: r }))}
          readOnly={readOnly}
        />
      ))}
    </>
  );
}

/** What kind of thing waits for review, in words. */
export function kindLabel(t: Translator, kind: ListingKind): string {
  switch (kind) {
    case "event":
      return t.t("Self-guided");
    case "party":
      return t.t("Party adventure");
    case "org":
      return t.t("Organisation page");
    default:
      return t.t("Adventure presentation");
  }
}

/** An organisation page or adventure presentation change, not a listing. */
export const isProposal = (kind: ListingKind) => kind === "org" || kind === "adventure";

function ReviewCard({ review: r, reviewed, onDone, onChanged, readOnly }: { review: ListingReview; reviewed?: ListingReview | undefined; onDone: () => void; onChanged: () => void; readOnly: boolean }) {
  const t = useT();
  const [rejecting, setRejecting] = useState(false);
  const { busy, approve, reject } = useDecide(onDone, onChanged);
  const d = r.draft;
  const live = r.live;
  const ref: ListingRef = r;

  const orgLink = href({ name: "organization", id: r.org_id });
  const publicLink = r.org_slug ? `/o/${encodeURIComponent(r.org_slug)}` : null;
  return (
    <Card
      title={
        <span className="adm-inline">
          {r.kind === "org" ? r.org_name : d.name || r.adventure_title}
          <Pill tone="info">{kindLabel(t, r.kind)}</Pill>
          {!isProposal(r.kind) && <Pill tone={live ? "warn" : "accent"}>{live ? t.t("Edit of a live listing") : t.t("New listing")}</Pill>}
          {live && live.graph_id !== d.graph_id && <Pill tone="warn">{t.t("New adventure version")}</Pill>}
        </span>
      }
      subtitle={
        <>
          <a href={orgLink}>{r.org_name}</a>
          {" · "}
          {r.adventure_title && <>{r.adventure_title}{" · "}</>}
          <span title={fmtDateTime(r.submitted_at, t.current())}>{t.t("submitted {{when}}", { when: relTime(r.submitted_at, t.current()) })}</span>
          {r.attested_by && <>{" · "}{t.t("by {{name}}", { name: r.attested_by })}</>}
        </>
      }
      actions={
        <span className="adm-actions">
          {publicLink && (
            <a className="adm-inline" href={publicLink} target="_blank" rel="noopener">
              {t.t("Public page")} <ExternalLink size={ICON.sm} aria-hidden />
            </a>
          )}
          <a className="btn sm" href={href({ name: "listing-review", kind: r.kind, id: r.id })}>
            <FileSearch size={ICON.sm} aria-hidden /> {t.t("Review in full")}
          </a>
          {!readOnly && <Button size="sm" variant="danger" icon={<X size={ICON.sm} aria-hidden />} disabled={busy} onClick={() => setRejecting(true)}>{t.t("Reject")}</Button>}
          {!readOnly && <Button size="sm" variant="primary" icon={<Check size={ICON.sm} aria-hidden />} loading={busy} onClick={() => void approve(ref)}>{t.t("Approve")}</Button>}
        </span>
      }
    >
      {isProposal(r.kind) ? (
        <p>{r.kind === "org"
          ? t.t("A change to the organisation's page. Open “Review in full” to compare it with what is public now.")
          : t.t("A change to the adventure's title, texts, icon or cover. Open “Review in full” to compare it with what is public now.")}</p>
      ) : (
        <ListingFacts kind={r.kind} draft={d} live={live} />
      )}
      {reviewed && reviewed.revision !== r.revision && (
        <>
          <p><Pill tone="warn">{t.t("Changed while you looked")}</Pill> {t.t("What changed since you opened it. Nothing was decided: look it over and decide again.")}</p>
          {!isProposal(r.kind) && <ListingFacts kind={r.kind} draft={d} live={reviewed.draft} />}
        </>
      )}
      {!readOnly && <p className="adm-sub">{r.kind === "org"
        ? t.t("Approve only once you have looked at all of it: open “Review in full” for every text and picture of the page.")
        : t.t("Approve only once you have looked at all of it: open “Review in full” for every text and picture of the adventure.")}</p>}
      {rejecting && (
        <RejectDialog
          proposal={isProposal(r.kind)}
          onClose={() => setRejecting(false)}
          onReject={(reason) => {
            setRejecting(false);
            void reject(ref, reason);
          }}
        />
      )}
    </Card>
  );
}

/** The listing as the public sees it, with what an edit changed. */
export function ListingFacts({ kind, draft: d, live }: { kind: ListingKind; draft: ListingFields; live?: ListingFields | undefined }) {
  const t = useT();
  const changed = (k: keyof ListingFields) => !!live && (live[k] ?? "") !== (d[k] ?? "");
  const money = (cents: number) => fmtMoney(cents, d.currency, t.current());
  return (
    <div className="adm-listing">
      {d.cover_url ? (
        <img className={"adm-listing__cover" + (changed("cover_url") ? " is-changed" : "")} src={d.cover_url} alt={t.t("Cover image")} />
      ) : live?.cover_url ? (
        <p className="adm-listing__cover adm-listing__none is-changed">{t.t("Cover removed")}</p>
      ) : null}
      <dl className="adm-listing__facts">
        {kind === "event" && (
          <Fact label={t.t("Name")} changed={changed("name")} was={live?.name}>{d.name || <span className="adm-sub">{t.t("(none)")}</span>}</Fact>
        )}
        <Fact label={t.t("Blurb")} changed={changed("blurb")} was={live?.blurb}>{d.blurb || <span className="adm-sub">{t.t("(none)")}</span>}</Fact>
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
    <div className={"adm-listing__fact" + (changed ? " is-changed" : "")}>
      <dt>
        {label}
        {changed && <Pill tone="warn">{t.t("Changed")}</Pill>}
      </dt>
      <dd>
        {children}
        {changed && was !== undefined && was !== "" && <span className="adm-sub adm-listing__was">{t.t("Was: {{v}}", { v: String(was) })}</span>}
      </dd>
    </div>
  );
}

/** `proposal`: an organisation page or adventure presentation change, not a listing. */
export function RejectDialog({ proposal = false, onClose, onReject }: { proposal?: boolean; onClose: () => void; onReject: (reason: string) => void }) {
  const t = useT();
  const [reason, setReason] = useState("");
  const ok = reason.trim() !== "";
  return (
    <Dialog
      title={proposal ? t.t("Reject this change?") : t.t("Reject this listing?")}
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
      <p className="adm-sub">{t.t("Say what to change and where. Whatever of theirs is public now stays up.")}</p>
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
  const { state } = useSession();
  const noEmail = state.status === "authenticated" && state.user.role === "admin" && !state.user.email;
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
      <span className="adm-stack">
        <strong>{e.title || kindLabel(t, e.kind)}</strong>
        <span className="adm-sub">{e.org_name}</span>
      </span>
    ) },
    { key: "action", header: t.t("Action"), cell: (e) => action(e.action) },
    { key: "via", header: t.t("By"), cell: (e) => (
      <span className="adm-inline"><ViaPill via={e.via} />{e.signed && <Pill tone="good">{t.t("Signed")}</Pill>}{e.by && <span className="adm-sub">{e.by}</span>}</span>
    ) },
    { key: "reason", header: t.t("Reason"), wide: true, cell: (e) => e.reason ? <span className="adm-sub">{e.reason}</span> : null },
  ];
  return (
    <Card
      title={t.t("Recent decisions")}
      subtitle={t.t("Spot-check what AI assistants approved and what verified creators put up without review.")}
      actions={
        <span className="adm-inline">
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
        <div className="adm-pad" role="status">
          {check.problems.length === 0 ? (
            <p><Pill tone="good">{t.t("Intact")}</Pill> {t.t("Every hash of the {{n}} entries adds up. The latest:", { n: check.entries })}</p>
          ) : (
            <>
              <p><Pill tone="bad">{t.t("Changed")}</Pill> {t.t("The log was changed after it was written:")}</p>
              <ul>{check.problems.map((p) => <li key={`${p.id}-${p.problem}`}>#{p.id}: {p.problem}</li>)}</ul>
              <p>{t.t("The latest hash now:")}</p>
            </>
          )}
          <p><code className="adm-hash">{check.head_hash || "—"}</code></p>
          <p>{check.signed === 1
            ? t.t("1 decision signed with a reviewer's passkey, checked again; {{unsigned}} unsigned (a reviewer without a passkey, an AI assistant or an automatic approval).", { unsigned: check.unsigned ?? 0 })
            : t.t("{{signed}} decisions signed with a reviewer's passkey, each checked again; {{unsigned}} unsigned (a reviewer without a passkey, an AI assistant or an automatic approval).", { signed: check.signed ?? 0, unsigned: check.unsigned ?? 0 })}</p>
          <p className="adm-sub">{t.t("Compare it with the fingerprint mailed to platform admins each day: if one mailed earlier can't be found in the log, it was rewritten.")}</p>
          {noEmail && (
            <p className="adm-sub">
              {t.t("Your account has no email address, so the daily fingerprint doesn't reach you.")}{" "}
              <a href={href({ name: "settings", tab: "account" })}>{t.t("Add email")}</a>
            </p>
          )}
          {check.served && <ServedResult served={check.served} />}
        </div>
      )}
      {log.error && !log.data ? (
        <ErrorBox message={log.error} retry={log.reload} retryLabel={t.t("Retry")} />
      ) : !log.data ? (
        <Loading label={t.t("Loading…")} />
      ) : log.data.length === 0 ? (
        <p className="adm-sub adm-pad">{t.t("Nothing yet.")}</p>
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
  const { refresh } = useSession();
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
      // The change may be the reader's own: the Waiting tab follows the session.
      void refresh();
    } catch (e) {
      toast.err(e instanceof ApiError && e.status === 404 ? t.t("No user is called {{name}}.", { name }) : e);
    } finally {
      setBusy(false);
    }
  };
  const columns: Column<ListingReviewer>[] = [
    { key: "name", header: t.t("Reviewer"), cell: (r) => (
      <span className="adm-stack">
        <strong className="adm-inline">{r.username}{r.is_admin && <Pill tone="accent">{t.t("Platform admin")}</Pill>}</strong>
        {r.email && <span className="adm-sub">{r.email}</span>}
      </span>
    ) },
    { key: "orgs", header: t.t("Never reviews"), wide: true, cell: (r) => r.orgs.length ? <span className="adm-sub">{r.orgs.join(", ")}</span> : <span className="adm-sub">{t.t("(no organisation)")}</span> },
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
      <form className="adm-row adm-pad" onSubmit={(e) => { e.preventDefault(); if (username.trim()) void set(username.trim(), true); }}>
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
        <p className="adm-sub adm-pad">{t.t("No one reviews listings yet.")}</p>
      ) : (
        <Table columns={columns} rows={list.data} rowKey={(r) => String(r.id)} caption={t.t("Reviewers")} />
      )}
    </Card>
  );
}

/**
 * The reviewer's passkeys, which, when they have one, sign every
 * decision they make in the console. The private key stays on their device (or in their passkey
 * manager); the server keeps only the public key.
 */
function Passkeys() {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const keys = useLoad((s) => listReviewerPasskeys(s), []);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const loc = t.current();

  const add = async () => {
    setBusy(true);
    try {
      const c = await beginReviewerPasskey();
      const credential = await createPasskey(c.options);
      await finishReviewerPasskey(c.session_id, name.trim() || t.t("Passkey"), credential);
      setName("");
      toast.ok(t.t("Passkey added: your decisions are signed with it now."));
      keys.reload();
    } catch (e) {
      toast.err(isCancelled(e) ? t.t("No passkey was added.") : reviewError(e, t));
    } finally {
      setBusy(false);
    }
  };
  const revoke = async (k: ReviewerPasskey) => {
    const ok = await dialogs.confirm({
      title: t.t("Remove “{{name}}”?", { name: k.name }),
      message: t.t("It can't sign decisions any more. Decisions it signed stay checkable: the log keeps its public key."),
      confirmLabel: t.t("Remove"),
      danger: true,
    });
    if (!ok) return;
    try {
      await revokeReviewerPasskey(k.id);
      keys.reload();
    } catch (e) {
      toast.err(reviewError(e, t));
    }
  };

  return (
    <Card
      title={t.t("Your passkeys")}
      subtitle={t.t("Optional: with a passkey, every decision you make in the console is signed with it. Your face, fingerprint or PIN unlocks it, and its private key never leaves your device or passkey manager. The server keeps only the public key, so no one can make or change a signed decision in your name.")}
    >
      {!passkeysSupported() && <ErrorBox message={t.t("This browser can't use passkeys. Use a recent browser on a device with a screen lock.")} />}
      {keys.error && !keys.data ? (
        <ErrorBox message={reviewError(new Error(keys.error), t)} retry={keys.reload} retryLabel={t.t("Retry")} />
      ) : !keys.data ? (
        <Loading label={t.t("Loading…")} />
      ) : keys.data.length === 0 ? (
        <p className="adm-sub">{t.t("You have no passkey: your decisions go in unsigned. Add one to sign them.")}</p>
      ) : (
        <ul className="adm-passkeys">
          {keys.data.map((k) => (
            <li key={k.id} className="adm-inline">
              <KeyRound size={ICON.sm} aria-hidden />
              <strong>{k.name}</strong>
              <span className="adm-sub" title={fmtDateTime(k.created_at, loc)}>{t.t("added {{when}}", { when: relTime(k.created_at, loc) })}</span>
              {k.last_used_at && (
                <span className="adm-sub" title={fmtDateTime(k.last_used_at, loc)}>{t.t("last signed {{when}}", { when: relTime(k.last_used_at, loc) })}</span>
              )}
              <Button size="sm" variant="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} onClick={() => void revoke(k)}>{t.t("Remove")}</Button>
            </li>
          ))}
        </ul>
      )}
      <form className="adm-inline" onSubmit={(e) => { e.preventDefault(); void add(); }}>
        <Field label={t.t("Name it (the device or manager it's in)")}>
          <input value={name} maxLength={100} placeholder={t.t("e.g. Work laptop")} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Button type="submit" variant="primary" icon={<KeyRound size={ICON.sm} aria-hidden />} loading={busy} disabled={!passkeysSupported()}>{t.t("Add a passkey")}</Button>
      </form>
    </Card>
  );
}

/** What approved listings serve now, against what was approved: from "Check the log". */
function ServedResult({ served }: { served: ServedReport }) {
  const t = useT();
  const part = (p: string) => ({
    "listing": t.t("the listing"),
    "adventure version": t.t("the adventure version"),
    "adventure content": t.t("the adventure's content"),
    "organisation page": t.t("the organisation's page"),
    "adventure presentation": t.t("the adventure's title, icon or description"),
    "event files": t.t("files for players"),
    "approval record": t.t("the approval record"),
    "the event plays another adventure version": t.t("the event plays another adventure version"),
  } as Record<string, string>)[p] ?? p;
  const changed = served.findings.filter((f) => f.severity === "changed");
  const unreviewed = served.findings.filter((f) => f.severity === "unreviewed");
  const unrecorded = served.findings.filter((f) => f.severity === "unrecorded");
  const row = (f: ServedFinding) => (
    <li key={`${f.kind}-${f.id}`}>
      <a href={href({ name: "listing-review", kind: f.kind, id: f.id })}>{f.title}</a> <span className="adm-sub">({f.org_name})</span>
      {f.parts && f.parts.length > 0 && <>: {f.parts.map(part).join(", ")}</>}
    </li>
  );
  return (
    <>
      <h3>{t.t("What approved listings serve")}</h3>
      {changed.length === 0 ? (
        <p><Pill tone="good">{t.t("As approved")}</Pill> {t.t("{{m}} of {{n}} approved listings serve exactly what was approved.", { m: served.matching, n: served.checked })}</p>
      ) : (
        <>
          <p><Pill tone="bad">{t.t("Changed outside review")}</Pill> {t.t("These serve something other than what was approved, though only an approval may change it:")}</p>
          <ul>{changed.map(row)}</ul>
        </>
      )}
      {unreviewed.length > 0 && (
        <>
          <p><Pill>{t.t("Edited since approval")}</Pill> {t.t("Organisations edit these live, and review doesn't hold them back yet. Have a look:")}</p>
          <ul>{unreviewed.map(row)}</ul>
        </>
      )}
      {unrecorded.length > 0 && (
        <p className="adm-sub">{t.t("{{n}} listings were for sale before review was logged, so there is no approval to compare them with.", { n: unrecorded.length })}</p>
      )}
    </>
  );
}
