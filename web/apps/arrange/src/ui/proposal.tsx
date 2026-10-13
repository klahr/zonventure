import { useState } from "react";
import { get, request } from "@zonventure/api";
import { useT } from "../i18n.tsx";
import { Button, Callout } from "../designer/dz/ui.tsx";
import { useDialogs } from "./dialogs.tsx";
import { SubmitListingDialog } from "./listingStatus.tsx";
import { useToast } from "./toast.tsx";
import "./listingStatus.css";

/**
 * A held-back change to an organisation's page or an adventure's
 * presentation (internal/admin/presentation.go): once something of
 * the organisation's is for sale, these edits wait for review, and
 * only an approval makes them public.
 */
export interface Proposal {
  /** 0 or absent when nothing is held back. */
  id?: number;
  state?: "draft" | "pending" | "rejected";
  fields: Record<string, unknown>;
  has_image: boolean;
  has_cover: boolean;
  reject_reason?: string;
  /** Edits to these fields wait for review now; otherwise they apply at once. */
  gated: boolean;
}

/** base is /api/admin/orgs/me/proposal or /api/admin/adventures/{slug}/proposal. */
export const getProposal = (base: string, signal?: AbortSignal) => get<Proposal>(base, signal ? { signal } : {});

/** A held-back field's proposed value as text, "" when it has none. */
export function proposedText(p: Proposal | null | undefined, key: string): string | undefined {
  const v = p?.fields?.[key];
  return typeof v === "string" ? v : undefined;
}

/**
 * Where a held-back change stands, what it proposes, and the means to
 * submit it for review — with the creator's word — or discard it.
 * labels names the proposed fields; pictures names the proposed
 * pictures by the URL suffix that serves them ("image", "cover").
 */
export function ProposalPanel({ kind, base, proposal, labels, pictures, onChanged }: {
  kind: "org" | "adventure";
  base: string;
  proposal: Proposal | null | undefined;
  labels: Record<string, string>;
  pictures: { key: "image" | "cover"; label: string }[];
  onChanged: () => void;
}) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [submitting, setSubmitting] = useState(false);
  const [version] = useState(() => Date.now());
  if (!proposal || (!proposal.gated && !proposal.id)) return null;

  const what = kind === "org"
    ? t.t("Something of yours is for sale, so changes to your tagline, website, logo and cover are reviewed before they go public. Colors apply at once, and removing a picture applies at once.")
    : t.t("This adventure is for sale, so changes to its title, description, author, intro, outro, meeting point, area, note to buyers, icon and cover are reviewed before players and buyers see them. Clearing a field applies at once.");
  if (!proposal.id) return <Callout variant="info">{what}</Callout>;

  const discard = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Discard changes"),
      message: t.t("Discard the changes waiting for review? What is public now stays."),
      confirmLabel: t.t("Discard"),
      danger: true,
    });
    if (!ok) return;
    try {
      await request<void>(base, { method: "DELETE" });
      toast.ok(t.t("Changes discarded"));
      onChanged();
    } catch (e) {
      toast.err(e);
    }
  };

  const fields = Object.entries(proposal.fields ?? {}).filter(([k]) => labels[k]);
  const state = proposal.state;
  return (
    <div className="ls-proposal">
      <Callout variant={state === "rejected" ? "danger" : state === "pending" ? "info" : "warning"}>
        {state === "pending" && t.t("In review: what is public now stays until your changes are approved.")}
        {state === "draft" && t.t("Not submitted: your changes are saved, and wait for you to submit them for review.")}
        {state === "rejected" && (
          <>
            <strong>{t.t("Not approved:")}</strong> {proposal.reject_reason} <br />
            {t.t("Change it, then submit it again. What is public now stays.")}
          </>
        )}
      </Callout>
      {(fields.length > 0 || pictures.some((p) => (p.key === "image" ? proposal.has_image : proposal.has_cover))) && (
        <dl className="ls-proposal__fields">
          {fields.map(([k, v]) => (
            <div key={k}>
              <dt>{labels[k]}</dt>
              <dd>{typeof v === "string" ? (v || t.t("(empty)")) : t.t("A new picture")}</dd>
            </div>
          ))}
          {pictures.filter((p) => (p.key === "image" ? proposal.has_image : proposal.has_cover)).map((p) => (
            <div key={p.key}>
              <dt>{p.label}</dt>
              <dd><img className="ls-proposal__img" src={`${base}/${p.key}?v=${version}`} alt="" /></dd>
            </div>
          ))}
        </dl>
      )}
      <div className="ls-proposal__actions">
        {state === "draft" && (
          <Button variant="primary" onClick={() => setSubmitting(true)}>{t.t("Submit for review")}</Button>
        )}
        <Button variant="ghost" onClick={() => void discard()}>{t.t("Discard changes")}</Button>
      </div>
      {submitting && (
        <SubmitListingDialog
          url={`${base}/submit`}
          note={kind === "org"
            ? t.t("A reviewer checks your organisation's page before the changes go public. What is public now stays up meanwhile.")
            : t.t("A reviewer checks the adventure's title, texts and pictures before the changes reach players and buyers. What is public now stays up meanwhile.")}
          attestText={t.t("I confirm these changes contain nothing offensive, hateful, sexual, violent or otherwise illegal or unsuitable, and follow the terms.")}
          onClose={() => setSubmitting(false)}
          onDone={() => onChanged()}
        />
      )}
    </div>
  );
}
