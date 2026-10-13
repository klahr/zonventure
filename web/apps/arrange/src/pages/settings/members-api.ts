import { get, post, put, request } from "@zonventure/api";

/** orgMemberDTO, internal/admin/organizations.go. */
export interface OrgMember {
  id: number;
  username: string;
  role: "org_admin" | "org_member" | "org_writer" | string;
  email_verified: boolean;
  totp_enabled: boolean;
  active_events_owned: number;
}

/** pendingInvitationDTO, internal/admin/organizations.go. */
export interface OrgInvitation {
  user_id: number;
  email: string;
  role: "org_admin" | "org_member" | "org_writer" | string;
  created_at: string;
  /** They said no; inviting them again asks anew. */
  declined?: boolean;
}

/** organizationDTO (the fields this tab reads), internal/admin/organizations.go. */
export interface OrgDetail {
  id: number;
  name: string;
  members: OrgMember[] | null;
  invitations?: OrgInvitation[] | null;
  require_totp: boolean;
}

export const getOrg = (id: number, signal?: AbortSignal) => get<OrgDetail>(`/api/admin/organizations/${id}`, signal ? { signal } : {});

/** Answers alike whether the email had an account or not: either way it gets an invitation. */
export const addMember = (orgID: number, body: { username: string; role: string; email: string }) =>
  post<{ status?: string } | undefined>(`/api/admin/organizations/${orgID}/members`, body);

export const setMemberRole = (orgID: number, userID: number, role: string, password: string) =>
  put<{ role: string }>(`/api/admin/organizations/${orgID}/members/${userID}/role`, { role }, { headers: { "X-Confirm-Password": password } });

export const withdrawInvitation = (orgID: number, userID: number) =>
  request<void>(`/api/admin/organizations/${orgID}/invitations/${userID}`, { method: "DELETE" });

export const removeMember = (orgID: number, userID: number, password: string) =>
  request<void>(`/api/admin/organizations/${orgID}/members/${userID}`, { method: "DELETE", headers: { "X-Confirm-Password": password } });

/** Turning it off needs the password; turning it on doesn't. */
export const setRequireTotp = (orgID: number, enabled: boolean, password?: string) =>
  put<{ require_totp: boolean }>(`/api/admin/organizations/${orgID}/require-totp`, { enabled }, password ? { headers: { "X-Confirm-Password": password } } : {});

export const transferOwnership =(orgID: number, toUserID: number, password: string) =>
  post<void>(`/api/admin/organizations/${orgID}/transfer-ownership`, { to_user_id: toUserID }, { headers: { "X-Confirm-Password": password } });
