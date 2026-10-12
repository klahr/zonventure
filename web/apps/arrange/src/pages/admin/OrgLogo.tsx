import { Avatar } from "../../ui/index.ts";

/** An organisation's uploaded logo, or its initials. `bust` forces a fresh copy after an upload. */
export function OrgLogo({ org, size, bust }: { org: { id: number; name: string; has_logo: boolean }; size: number; bust?: number }) {
  if (org.has_logo) {
    return <img className="adm-logo" src={`/org-logo/${org.id}${bust ? `?t=${bust}` : ""}`} alt="" style={{ width: size, height: size }} />;
  }
  return <Avatar name={org.name} size={size} />;
}
