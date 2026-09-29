import { get, post, request } from "@zonventure/api";

/** orgMemberDTO, internal/admin/organizations.go. */
export interface OrgMember {
  id: number;
  username: string;
  role: "org_admin" | "org_member" | string;
  email_verified: boolean;
  active_events_owned: number;
}

/** organizationDTO (the fields this tab reads), internal/admin/organizations.go. */
export interface OrgDetail {
  id: number;
  name: string;
  members: OrgMember[] | null;
}

/** userSearchMatch, internal/admin/users.go. */
export interface UserMatch {
  id: number;
  username: string;
}

export const getOrg = (id: number, signal?: AbortSignal) => get<OrgDetail>(`/api/admin/organizations/${id}`, signal ? { signal } : {});

export const searchUsers = (q: string, orgID: number, signal?: AbortSignal) =>
  get<{ users: UserMatch[] | null }>(`/api/admin/users/search?q=${encodeURIComponent(q)}&org_id=${orgID}`, signal ? { signal } : {});

export const addMember = (orgID: number, body: { username: string; role: string; password?: string; email?: string }) =>
  post<unknown>(`/api/admin/organizations/${orgID}/members`, body);

export const removeMember = (orgID: number, userID: number, password: string) =>
  request<void>(`/api/admin/organizations/${orgID}/members/${userID}`, { method: "DELETE", headers: { "X-Confirm-Password": password } });

export const transferOwnership = (orgID: number, toUserID: number, password: string) =>
  post<void>(`/api/admin/organizations/${orgID}/transfer-ownership`, { to_user_id: toUserID }, { headers: { "X-Confirm-Password": password } });
