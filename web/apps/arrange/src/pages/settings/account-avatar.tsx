import { useState } from "react";
import type { Whoami } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Card, ImageDrop, errorText } from "../../ui/index.ts";
import { account } from "./account-api.ts";

/**
 * The person's own picture: shown in the account menu, beside their
 * comments, on the steps they own and wherever others see them working.
 * Without one they are their initials on their colour.
 */
export function AvatarCard({ user }: { user: Whoami }) {
  const t = useT();
  const { refresh } = useSession();
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
        <ImageDrop
          shape="photo"
          src={user.avatar_url || undefined}
          label={t.t("Choose a picture")}
          accept="image/png,image/jpeg,image/webp,image/svg+xml"
          busy={busy}
          onFile={(f) => void act(() => account.uploadAvatar(f))}
          onRemove={() => void act(() => account.removeAvatar())}
        />
        <p className="acct-avatar__hint">{t.t("A square photo works best. PNG, JPEG, WebP or SVG, up to 5 MB.")}</p>
      </div>
      {err && <p className="cs-field__error" role="alert">{err}</p>}
    </Card>
  );
}
