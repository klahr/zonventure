// Auth endpoints. One thin function per route — the types are the
// contract, and these exist so call sites read as intent rather than
// as string literals sprinkled through components.
//
// Routes: internal/server/routes_auth.go

import { get, post } from "./http.ts";
import type { LoginRequest, LoginResponse, SignupConfig, Whoami } from "./types.ts";

/**
 * Log in. Resolves with code "totp_required" and a challenge when the
 * account has a second factor — a successful first step, not an error,
 * so it comes back in the body rather than as a thrown ApiError.
 */
export const login = (body: LoginRequest) =>
  post<LoginResponse>("/api/auth/login", body);

/** Complete a login that reported totp_required: the code, or a recovery code. */
export const loginTOTP = (challenge: string, answer: { code: string } | { recovery_code: string }) =>
  post<void>("/api/auth/login/totp", { challenge, ...answer });

/** Whether sign-up is open, and the single sign-on button if configured. */
export const signupConfig = () => get<SignupConfig>("/api/auth/signup-config");

export const signup = (body: { email: string; org_name: string; password: string; terms_accepted: boolean }) =>
  post<void>("/api/auth/signup", body);

/** Confirm an email change from the link in the mail. */
export const confirmEmailChange = (token: string) =>
  post<{ email?: string }>("/api/auth/confirm-email-change", { token });

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

/** Set a new password from a reset link. Signs in, or asks for the second factor. */
export const resetPassword = (token: string, password: string) =>
  post<LoginResponse | undefined>("/api/auth/reset", { token, new_password: password });

/** Pin the session's active organisation. */
export const switchOrg = (orgID: number) =>
  post<void>("/api/auth/switch-org", { org_id: orgID });
