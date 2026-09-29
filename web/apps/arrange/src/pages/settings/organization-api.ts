import { get, put, request } from "@zonventure/api";

/** GET/PUT /api/admin/orgs/me/profile — orgProfileDTO, internal/admin/org_profile.go. */
export interface OrgProfile {
  slug: string;
  store_url: string;
  landing_url: string;
  tagline: string;
  public_website: string;
  has_logo: boolean;
  has_cover: boolean;
  accent_color?: string;
  accent_secondary_color?: string;
  background_color?: string;
  text_color?: string;
}

export type ProfileEdit = Pick<OrgProfile, "tagline" | "public_website"> & {
  accent_color: string;
  accent_secondary_color: string;
  background_color: string;
  text_color: string;
};

export const getProfile = (signal?: AbortSignal) => get<OrgProfile>("/api/admin/orgs/me/profile", signal ? { signal } : {});
export const putProfile = (p: ProfileEdit) => put<OrgProfile>("/api/admin/orgs/me/profile", p);
/** orgSlugDTO, internal/admin/organizations.go. */
export const putSlug = (slug: string) => put<{ slug: string; store_url: string }>("/api/admin/orgs/me/slug", { slug });

export function uploadImage(kind: "logo" | "cover", file: File) {
  const form = new FormData();
  form.append("file", file);
  return request<unknown>(`/api/admin/orgs/me/${kind}`, { method: "POST", form });
}

export const deleteImage = (kind: "logo" | "cover") => request<void>(`/api/admin/orgs/me/${kind}`, { method: "DELETE" });
