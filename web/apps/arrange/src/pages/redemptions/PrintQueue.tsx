import { useState } from "react";
import { MailCheck, Printer } from "lucide-react";
import { play } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, ICON, useToast } from "../../ui/index.ts";
import { updatePurchase, updateRedemption, type Redemption } from "./api.ts";
import { langName, useRedemptionError } from "./shared.ts";

/** An order with letters to print. A pending code isn't checked yet: nothing is printed for it. */
export const printable = (o: Redemption) => (o.state === "approved" || o.state === "paid") && (o.print?.length ?? 0) > 0;

/**
 * Prints the letters of the orders shown, one after the other. A browser
 * opens one new tab per click, so each click opens the next PDF to print;
 * the queue keeps count. At the end, the orders can be marked posted in
 * one go.
 */
export function PrintQueueButton({ orders, reload }: { orders: Redemption[]; reload: () => void }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const live = orders.filter(printable);
  if (live.length === 0) return null;
  const docs = live.reduce((n, o) => n + (o.print?.length ?? 0), 0);
  return (
    <>
      <Button size="sm" icon={<Printer size={ICON.sm} aria-hidden />} onClick={() => setOpen(true)}>
        {t.plural(docs, { one: t.t("Print queue: {{n}} document"), other: t.t("Print queue: {{n}} documents") })}
      </Button>
      {open && <PrintQueueDialog orders={live} onClose={() => setOpen(false)} reload={reload} />}
    </>
  );
}

function PrintQueueDialog({ orders, onClose, reload }: { orders: Redemption[]; onClose: () => void; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const errText = useRedemptionError();
  const locale = t.current();
  const items = orders.flatMap((o) => (o.print ?? []).map((p) => ({ order: o, doc: p })));
  const [next, setNext] = useState(0);
  const [busy, setBusy] = useState(false);
  const done = next >= items.length;
  const current = items[next];
  const unposted = orders.filter((o) => !o.posted_at);

  const openNext = () => {
    if (!current) return;
    window.open(play.mediaURL(current.doc.media_id), "_blank", "noopener");
    setNext(next + 1);
  };
  const markPosted = async () => {
    setBusy(true);
    try {
      for (const o of unposted) {
        await (o.kind === "purchase" ? updatePurchase(o.id, { posted: true }) : updateRedemption(o.id, { posted: true }));
      }
      toast.ok(t.plural(unposted.length, { one: t.t("{{n}} order marked as posted"), other: t.t("{{n}} orders marked as posted") }));
      reload();
      onClose();
    } catch (e) {
      toast.err(errText(e));
      reload();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={t.t("Print queue")}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Close")}</Button>
          {done ? (
            unposted.length > 0 && (
              <Button variant="primary" loading={busy} icon={<MailCheck size={ICON.sm} aria-hidden />} onClick={() => void markPosted()}>
                {t.plural(unposted.length, { one: t.t("Mark {{n}} order as posted"), other: t.t("Mark these {{n}} orders as posted") })}
              </Button>
            )
          ) : (
            <Button variant="primary" icon={<Printer size={ICON.sm} aria-hidden />} onClick={openNext} autoFocus>
              {t.t("Open {{i}} of {{n}}", { i: next + 1, n: items.length })}
            </Button>
          )}
        </>
      }
    >
      {current ? (
        <p>
          {t.t("Next: {{doc}} for {{name}}", {
            doc: (current.order.letter_languages?.length ?? 0) > 1 ? `${current.doc.name} (${langName(current.doc.lang, locale)})` : current.doc.name,
            name: current.order.name || current.order.email,
          })}
        </p>
      ) : (
        <p>{t.t("All {{n}} opened. Print them, post the envelopes, then mark the orders as posted.", { n: items.length })}</p>
      )}
      <p className="rd-sub">{t.t("Each click opens one PDF in a new tab: browsers don't let one click open more.")}</p>
    </Dialog>
  );
}
