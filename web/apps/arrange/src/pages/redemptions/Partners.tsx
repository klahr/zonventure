import { useState } from "react";
import { Copy, ExternalLink, Pencil, Plus, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Card, Field, HelpPopover, ICON, Pill, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { MoreMenu } from "../event/Menu.tsx";
import { deletePartner, deleteProduct, listSelfGuided, orgSlug, setPartnerEnabled, updateProduct, type Partner, type PartnerProduct, type SelfGuidedEvent } from "./api.ts";
import { PartnerDialog, ProductDialog } from "./dialogs.tsx";
import { useRedemptionError } from "./shared.ts";

/**
 * The setup side: which partners' codes the organisation takes, and
 * which self-guided adventure each of their products unlocks. Org
 * admins edit it; members see it read-only.
 */
export function PartnersCard({ partners, isOrgAdmin, reload }: { partners: Partner[]; isOrgAdmin: boolean; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const errText = useRedemptionError();
  const slug = useLoad((s) => orgSlug(s).catch(() => ""), []);
  const [editing, setEditing] = useState<{ partner: Partner; existing: PartnerProduct | null; events: SelfGuidedEvent[] } | null>(null);
  /** The partner being set up: null adds one, undefined is no dialog. */
  const [settingUp, setSettingUp] = useState<Partner | null | undefined>(undefined);
  const url = location.origin + (slug.data ? `/o/${slug.data}/redeem` : "/redeem");

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn();
      toast.ok(ok);
      reload();
    } catch (e) {
      toast.err(errText(e));
    }
  };

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(url);
      toast.ok(t.t("Link copied"));
    } catch {
      toast.err(t.t("Copy failed"));
    }
  };

  const openProduct = async (partner: Partner, existing: PartnerProduct | null) => {
    try {
      const events = (await listSelfGuided()).filter((e) => e.state !== "cancelled" && e.state !== "completed");
      if (events.length === 0 && !existing) { toast.err(t.t("Create a self-guided adventure first")); return; }
      setEditing({ partner, existing, events });
    } catch (e) {
      toast.err(e);
    }
  };

  const removePartner = async (p: Partner) => {
    const ok = await dialogs.confirm({
      title: t.t("Delete {{partner}}?", { partner: p.name }),
      message: t.t("Its products go too, and customers can no longer redeem its codes."),
      confirmLabel: t.t("Delete"),
      danger: true,
    });
    if (ok) await run(() => deletePartner(p.id), t.t("Partner deleted"));
  };

  const removeProduct = async (x: PartnerProduct) => {
    const ok = await dialogs.confirm({
      title: t.t("Delete product “{{name}}”?", { name: x.name || x.event_name || "" }),
      message: t.t("Customers can no longer pick it on the redeem page."),
      confirmLabel: t.t("Delete"),
      danger: true,
    });
    if (ok) await run(() => deleteProduct(x.id), t.t("Product deleted"));
  };

  return (
    <Card
      title={
        <span className="rd-title">
          {t.t("Partners")}
          <HelpPopover label={t.t("About partners")} body={t.t("Gift box partners whose codes customers can redeem, and which of your self-guided adventures each of their products gives access to.")} />
        </span>
      }
    >
      {partners.length > 0 && (
        <Field label={t.t("Public redeem page")}>
          <div className="rd-url">
            <input type="text" readOnly value={url} onFocus={(e) => e.target.select()} />
            <a className="btn" href={url} target="_blank" rel="noopener"><ExternalLink size={ICON.md} aria-hidden />{t.t("Open page")}</a>
            <Button icon={<Copy size={ICON.md} aria-hidden />} onClick={copyLink}>{t.t("Copy link")}</Button>
          </div>
        </Field>
      )}

      {partners.length === 0 && (
        <p className="rd-muted">{isOrgAdmin ? t.t("No partners yet. Add one to start taking its codes.") : t.t("No partners yet. An organization admin can add them.")}</p>
      )}

      {partners.map((p) => {
        const products = p.products ?? [];
        return (
          <section key={p.id} className="rd-partner" aria-label={p.name}>
            <div className="rd-partner__head">
              <h3 className="rd-partner__title">
                {p.name}
                {!p.enabled && <Pill>{t.t("Disabled")}</Pill>}
              </h3>
              {isOrgAdmin && (
                <span className="rd-actions">
                  <Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} onClick={() => openProduct(p, null)}>{t.t("Add product")}</Button>
                  <MoreMenu size="sm"
                    label={t.t("More")}
                    items={[
                      { label: t.t("Edit partner"), icon: <Pencil size={ICON.sm} aria-hidden />, onClick: () => setSettingUp(p) },
                      { label: p.enabled ? t.t("Disable") : t.t("Enable"), onClick: () => void run(() => setPartnerEnabled(p.id, !p.enabled), p.enabled ? t.t("Partner disabled") : t.t("Partner enabled")) },
                      { separator: true },
                      { label: t.t("Delete partner"), icon: <Trash2 size={ICON.sm} aria-hidden />, danger: true, onClick: () => void removePartner(p) },
                    ]}
                  />
                </span>
              )}
            </div>
            {products.length === 0 ? (
              <p className="rd-muted">{t.t("No products yet: customers can't redeem this partner's codes until one points at a self-guided adventure.")}</p>
            ) : (
              <ul className="rd-products">
                {products.map((x) => (
                  <li key={x.id} className={"rd-product" + (x.enabled ? "" : " is-off")}>
                    <span className="rd-product__text">
                      <strong>{x.name || "—"}</strong>
                      <span className="rd-sub">
                        <a href={href({ name: "event", id: x.event_id })}>{x.event_name || `#${x.event_id}`}</a>
                        {x.adventure_title && <> · {x.adventure_title}</>}
                        {" · "}
                        {t.plural(x.team_size, { one: t.t("{{n}} player per team"), other: t.t("{{n}} players per team") })}
                      </span>
                    </span>
                    {!x.enabled && <Pill>{t.t("Disabled")}</Pill>}
                    {isOrgAdmin && (
                      <span className="rd-actions">
                        <Button size="icon" variant="ghost" icon={<Pencil size={ICON.sm} aria-hidden />} aria-label={t.t("Edit product")} title={t.t("Edit product")} onClick={() => openProduct(p, x)} />
                        <Button size="sm" variant="ghost" onClick={() => run(() => updateProduct(x.id, { enabled: !x.enabled }), t.t("Saved"))}>{x.enabled ? t.t("Disable") : t.t("Enable")}</Button>
                        <Button size="icon" variant="ghost" className="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Delete product")} title={t.t("Delete product")} onClick={() => removeProduct(x)} />
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>
        );
      })}

      {isOrgAdmin && (
        <div className="rd-actions rd-actions--start">
          <Button icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setSettingUp(null)}>{t.t("Add partner")}</Button>
        </div>
      )}

      {settingUp !== undefined && (
        <PartnerDialog
          existing={settingUp}
          onClose={() => setSettingUp(undefined)}
          onDone={() => { setSettingUp(undefined); reload(); }}
        />
      )}

      {editing && (
        <ProductDialog
          partner={editing.partner}
          existing={editing.existing}
          events={editing.events}
          onClose={() => setEditing(null)}
          onDone={() => { setEditing(null); reload(); }}
        />
      )}
    </Card>
  );
}
