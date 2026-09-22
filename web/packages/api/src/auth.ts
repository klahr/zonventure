// Auth endpoints. One thin function per route — the types are the
// contract, and these exist so call sites read as intent rather than
// as string literals sprinkled through components.
//
// Routes: internal/server/routes_auth.go

import { get, post } from "./http.ts";
import type { LoginRequest, LoginResponse, Whoami } from "./types.ts";

/**
 * Log in. Resolves with `totp_required: true` when the account has a
 * second factor — that is a successful first step, not an error, so it
 * comes back in the body rather than as a thrown ApiError.
 */
export const login = (body: LoginRequest) =>
  post<LoginResponse>("/api/auth/login", body);

/** Complete a login that reported `totp_required`. */
export const loginTOTP = (code: string) =>
  post<void>("/api/auth/login/totp", { code });

export const logout = () => post<void>("/api/auth/logout");

/**
 * The current session. Throws ApiError 401 when not logged in, which
 * is the normal anonymous case and callers should treat as a state,
 * not a failure.
 */
export const whoami = (signal?: AbortSignal) =>
  get<Whoami>("/api/auth/whoami", signal ? { signal } : {});

export const forgotPassword = (email: string) =>
  post<void>("/api/auth/forgot", { email });

export const resetPassword = (token: string, password: string) =>
  post<void>("/api/auth/reset", { token, password });

/** Pin the session's active organisation. */
export const switchOrg = (orgID: number) =>
  post<void>("/api/auth/switch-org", { org_id: orgID });
