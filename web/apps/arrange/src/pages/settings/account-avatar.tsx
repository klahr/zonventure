import { useRef, useState } from "react";
import { ImagePlus, Trash2 } from "lucide-react";
import type { Whoami } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, PersonAvatar, errorText } from "../../ui/index.ts";
import { peerColor } from "../../ui/presence.tsx";
import { account } from "./account-api.ts";

/**
 * The person's own picture: shown in the account menu, beside their
 * comments, on the steps they own and wherever others see them working.
 * Without one they are their initials on their colour.
 */
export function AvatarCard({ user }: { user: Whoami }) {
  const t = useT();
  const { refresh } = useSession();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      setErr(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title={t.t("Your picture")} subtitle={t.t("Others see it beside your comments, on the steps you own and when you work on the same adventure.")}>
      <div className="acct-avatar">
        <button type="button" className="acct-avatar__pick" onClick={() => input.current?.click()} disabled={busy} aria-label={t.t("Choose a picture")} title={t.t("Choose a picture")}>
          <PersonAvatar name={user.username} src={user.avatar_url} size={72} color={peerColor({ user_id: user.id, kind: "human" })} />
          <span className="acct-avatar__overlay" aria-hidden>
            <ImagePlus size={20} />
          </span>
        </button>
        <div className="acct-avatar__actions">
          <Button size="sm" icon={<ImagePlus size={14} aria-hidden />} loading={busy} onClick={() => input.current?.click()}>
            {user.avatar_url ? t.t("Change picture") : t.t("Upload a picture")}
          </Button>
          {user.avatar_url && (
            <Button size="sm" variant="ghost" icon={<Trash2 size={14} aria-hidden />} disabled={busy} onClick={() => void act(() => account.removeAvatar())}>
              {t.t("Remove")}
            </Button>
          )}
          <p className="acct-avatar__hint">{t.t("A square photo works best. PNG, JPEG, WebP or SVG, up to 5 MB.")}</p>
        </div>
        <input
          ref={input}
          type="file"
          hidden
          accept="image/png,image/jpeg,image/webp,image/svg+xml"
          onChange={(e) => {
            const f = e.target.files?.[0];
            e.target.value = "";
            if (f) void act(() => account.uploadAvatar(f));
          }}
        />
      </div>
      {err && <p className="cs-field__error" role="alert">{err}</p>}
    </Card>
  );
}
