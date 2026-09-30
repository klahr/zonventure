import { useEffect, useRef, useState } from "react";
import { MapPin } from "lucide-react";
import { metresBetween } from "@zonventure/graph";
import { useT } from "../../i18n.tsx";
import { locationError } from "../../designer/zones.ts";
import { Button, Callout, ICON, Page, PageHeader } from "../../ui/index.ts";
import { putMyPosition } from "./api.ts";

/** Send a fix when the phone has moved this far… */
const MOVE_M = 3;
/** …or this long after the last one, so a standing author stays fresh. */
const REFRESH_MS = 20_000;

/**
 * Field mode: the phone tells the author's assistant where they stand,
 * so "put a stop here" works while building an adventure on site. Only
 * the latest fix is kept on the server (internal/admin/position.go).
 */
export function FieldPage() {
  const t = useT();
  const [on, setOn] = useState(false);
  const [fix, setFix] = useState<{ accuracy: number; sentAt: number } | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const last = useRef<{ lat: number; lng: number; at: number } | null>(null);

  useEffect(() => {
    if (!on) return;
    if (typeof navigator === "undefined" || !navigator.geolocation) {
      setOn(false);
      setProblem(t.t("This browser can't tell where you are."));
      return;
    }
    const id = navigator.geolocation.watchPosition(
      (p) => {
        const here = { lat: p.coords.latitude, lng: p.coords.longitude, at: Date.now() };
        const prev = last.current;
        if (prev && here.at - prev.at < REFRESH_MS && metresBetween(prev.lat, prev.lng, here.lat, here.lng) < MOVE_M) return;
        last.current = here;
        const accuracy = Math.max(1, Math.round(p.coords.accuracy));
        putMyPosition({ lat: here.lat, lng: here.lng, accuracy_m: accuracy }).then(
          () => {
            setProblem(null);
            setFix({ accuracy, sentAt: Date.now() });
          },
          () => setProblem(t.t("Your position couldn't be sent. Check the connection.")),
        );
      },
      (e) => {
        setOn(false);
        setProblem(locationError(e, t));
      },
      { enableHighAccuracy: true, maximumAge: 5000 },
    );
    // A phone whose screen goes dark stops reporting; keep it lit while sharing.
    let lock: WakeLockSentinel | null = null;
    const wake = () => {
      if (document.visibilityState === "visible" && "wakeLock" in navigator) {
        navigator.wakeLock.request("screen").then((l) => (lock = l), () => {});
      }
    };
    wake();
    document.addEventListener("visibilitychange", wake);
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => {
      navigator.geolocation.clearWatch(id);
      document.removeEventListener("visibilitychange", wake);
      void lock?.release();
      clearInterval(tick);
      last.current = null;
    };
  }, [on, t]);

  const age = fix ? Math.max(0, Math.round((now - fix.sentAt) / 1000)) : null;
  return (
    <Page>
      <PageHeader
        title={t.t("Field mode")}
        subtitle={t.t("Keep this page open while you walk. Your assistant uses it to put stops where you stand.")}
        back={{ href: "#/adventures", label: t.t("Adventures") }}
      />
      <div className="adv-field">
        <Button
          variant="primary"
          filled={!on}
          icon={<MapPin size={ICON.md} aria-hidden />}
          onClick={() => {
            setProblem(null);
            setOn(!on);
          }}
        >
          {on ? t.t("Stop sharing my location") : t.t("Share my location")}
        </Button>
        <p className="adv-field__status" aria-live="polite">
          {!on
            ? t.t("Not sharing.")
            : fix && age !== null
              ? t.t("Sharing, to within about {{m}} m. Last sent {{s}} s ago.", { m: fix.accuracy, s: age })
              : t.t("Finding you…")}
        </p>
        {problem && <Callout variant="warning">{problem}</Callout>}
        <p className="adv-field__note">{t.t("Only your latest position is kept, and only your assistant reads it.")}</p>
      </div>
    </Page>
  );
}
