import { useEffect } from "react";
import { useT } from "../../i18n.tsx";
import { useToast } from "../../ui/index.ts";
import { navigate } from "../../router.ts";
import { getPurchase } from "./api.ts";

const KEY = "zv-just-purchased";

/**
 * The way back from a checkout: reopen the new-event dialog on the pass
 * that was just paid for. Two signals, read once and cleared so a
 * reload doesn't reopen it:
 *
 *  - sessionStorage "zv-just-purchased" holding the minted assignment
 *    id, set by the simulated checkout page (internal/admin/purchases.go)
 *    and by the classic console's buy flow;
 *  - a "#purchase-success/{id}" hash, Stripe's success URL
 *    (internal/admin/stripe.go). The webhook marks the purchase paid,
 *    which can trail the redirect, so the purchase is polled briefly.
 */
export function useJustPurchased(open: (assignmentId: number) => void) {
  const t = useT();
  const toast = useToast();
  useEffect(() => {
    let stored: string | null = null;
    try {
      stored = sessionStorage.getItem(KEY);
      if (stored) sessionStorage.removeItem(KEY);
    } catch {
      /* storage blocked: nothing was stashed */
    }
    const id = stored ? parseInt(stored, 10) : NaN;
    if (id > 0) {
      toast.ok(t.t("Payment received — let's schedule your event."));
      open(id);
      return;
    }

    const m = /^#\/?purchase-success\/(\d+)/.exec(location.hash);
    if (!m) return;
    // Drop the hash once done, unless the user has moved on meanwhile.
    const done = () => {
      if (location.hash.startsWith(m[0])) history.replaceState(null, "", location.pathname + location.search + "#/");
    };
    const purchaseId = Number(m[1]);
    const ac = new AbortController();
    let tries = 0;
    const poll = async () => {
      try {
        const p = await getPurchase(purchaseId, ac.signal);
        if (ac.signal.aborted) return;
        if (p.state === "paid" && p.assignment_id) {
          done();
          toast.ok(t.t("Payment received — let's schedule your event."));
          open(p.assignment_id);
          return;
        }
      } catch {
        if (ac.signal.aborted) return;
      }
      if (++tries < 10) timer = setTimeout(() => void poll(), 1500);
      else {
        done();
        toast.ok(t.t("Payment is being confirmed. The event pass will appear under Settings → Purchases shortly."));
      }
    };
    let timer: ReturnType<typeof setTimeout> | undefined;
    void poll();
    return () => {
      ac.abort();
      clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}

/**
 * The way back from paying to host an adventure: the checkout returns
 * to the console with ?paid= (internal/admin/hosted_pricing.go). Says
 * the payment came through and where the game and its host link are.
 */
export function useHostedPaid() {
  const t = useT();
  const toast = useToast();
  useEffect(() => {
    if (!new URLSearchParams(location.search).has("paid")) return;
    history.replaceState(null, "", location.pathname);
    toast.ok(t.t("Payment received. The host link is on its way to your email, and the game is under Hosted."));
    navigate({ name: "parties" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}
