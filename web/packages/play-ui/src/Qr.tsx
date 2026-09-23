import { useEffect, useRef, useState } from "react";
import type { Translate } from "./feedback.ts";

/**
 * Answer by scanning a QR code, as play.html allowed. The decoded text is
 * submitted as a guess, so an author prints a code of any accepted
 * answer and hangs it at the place.
 *
 * Decoding uses the browser's BarcodeDetector where it exists and the
 * vendored jsQR otherwise (/assets/jsqr.js, loaded only when a scan
 * starts, so the ~40 KB never reaches a player who does not scan).
 */
export function QrButton({ t, disabled, onScan }: { t: Translate; disabled?: boolean | undefined; onScan: (text: string) => void }) {
  const [open, setOpen] = useState(false);
  if (typeof navigator === "undefined" || !navigator.mediaDevices?.getUserMedia) return null;
  return (
    <>
      <button type="button" className="pv-btn pv-qr-btn" disabled={disabled} onClick={() => setOpen(true)} aria-label={t("Scan a QR code")}>
        <svg viewBox="0 0 24 24" aria-hidden>
          <path d="M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h3v3h-3zM18 18h3v3h-3zM14 19h2M19 14h2" />
        </svg>
      </button>
      {open && (
        <Scanner
          t={t}
          onClose={() => setOpen(false)}
          onScan={(text) => {
            setOpen(false);
            onScan(text);
          }}
        />
      )}
    </>
  );
}

type Decode = (video: HTMLVideoElement, canvas: HTMLCanvasElement) => Promise<string | null>;

interface BarcodeDetectorLike {
  detect(source: HTMLVideoElement): Promise<{ rawValue: string }[]>;
}

async function decoder(): Promise<Decode> {
  const BD = (globalThis as { BarcodeDetector?: new (o: { formats: string[] }) => BarcodeDetectorLike }).BarcodeDetector;
  if (BD) {
    const d = new BD({ formats: ["qr_code"] });
    return async (video) => (await d.detect(video))[0]?.rawValue ?? null;
  }
  const jsQR = await loadJsqr();
  return async (video, canvas) => {
    const w = video.videoWidth;
    const h = video.videoHeight;
    if (!w || !h) return null;
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext("2d", { willReadFrequently: true });
    if (!ctx) return null;
    ctx.drawImage(video, 0, 0, w, h);
    return jsQR(ctx.getImageData(0, 0, w, h).data, w, h)?.data ?? null;
  };
}

type JsQR = (data: Uint8ClampedArray, w: number, h: number) => { data: string } | null;
let jsqrLoading: Promise<JsQR> | null = null;

function loadJsqr(): Promise<JsQR> {
  const existing = (globalThis as { jsQR?: JsQR }).jsQR;
  if (existing) return Promise.resolve(existing);
  jsqrLoading ??= new Promise<JsQR>((resolve, reject) => {
    const s = document.createElement("script");
    s.src = "/assets/jsqr.js";
    s.async = true;
    s.onload = () => {
      const f = (globalThis as { jsQR?: JsQR }).jsQR;
      if (f) resolve(f);
      else reject(new Error("jsQR missing"));
    };
    s.onerror = () => {
      jsqrLoading = null;
      reject(new Error("jsQR failed to load"));
    };
    document.head.append(s);
  });
  return jsqrLoading;
}

function Scanner({ t, onClose, onScan }: { t: Translate; onClose: () => void; onScan: (text: string) => void }) {
  const video = useRef<HTMLVideoElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [status, setStatus] = useState(t("Point the camera at the code."));
  const scanRef = useRef(onScan);
  scanRef.current = onScan;
  // Callers pass a fresh translator each render; the camera must not
  // restart for that.
  const tRef = useRef(t);
  tRef.current = t;

  useEffect(() => {
    let stream: MediaStream | null = null;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    void (async () => {
      try {
        stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" }, audio: false });
        if (stopped || !video.current) {
          // Closed while the permission prompt was up: release the camera.
          stream.getTracks().forEach((tr) => tr.stop());
          return;
        }
        video.current.srcObject = stream;
        await video.current.play();
        const decode = await decoder();
        // A few frames a second is plenty to catch a code, and a
        // phone's camera and battery are already working hard.
        const tick = async () => {
          if (stopped || !video.current || !canvas.current) return;
          const text = await decode(video.current, canvas.current).catch(() => null);
          if (text && !stopped) {
            scanRef.current(text.trim());
            return;
          }
          timer = setTimeout(() => void tick(), 250);
        };
        void tick();
      } catch {
        if (!stopped) setStatus(tRef.current("The camera is not available. Allow camera access, or type the answer."));
      }
    })();

    return () => {
      stopped = true;
      clearTimeout(timer);
      stream?.getTracks().forEach((tr) => tr.stop());
    };
  }, []);

  return (
    <div className="pv-qr" role="dialog" aria-modal="true" aria-label={t("Scan a QR code")}>
      <video ref={video} playsInline muted />
      <canvas ref={canvas} hidden />
      <div className="pv-qr__frame" aria-hidden />
      <p className="pv-qr__status" role="status">
        {status}
      </p>
      <button type="button" className="pv-qr__cancel" onClick={onClose}>
        {t("Cancel")}
      </button>
    </div>
  );
}
