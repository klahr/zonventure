// Types mirroring the Go JSON shapes.
//
// Hand-written for now, and only for what Phase 1 actually calls. The
// plan's longer-term answer is codegen from the Go structs (tygo,
// committed output, CI-gated on `git diff --exit-code`) because the
// server already tags every field correctly and so costs nothing to
// generate from. That earns its keep once there are dozens of types to
// keep in sync; at four it would be more machinery than benefit.
//
// Source of truth: internal/auth/handlers.go.

/** internal/auth/handlers.go: membershipDTO */
export interface Membership {
  org_id: number;
  org_name: string;
  role: string;
  /** Canonical /org-logo/{id} path; absent when the org has no logo. */
  org_logo_url?: string;
  trusted?: boolean;
}

/** internal/auth/handlers.go: whoamiResponse */
export interface Whoami {
  id: number;
  username: string;
  /** Their own picture, absent while they have none. */
  avatar_url?: string;
  role: Role;
  org_id?: number;
  email?: string;
  email_verified: boolean;
  /** Full org list with the role held in each. Empty for admins. */
  memberships: Membership[];
  /** RFC3339, set only when account deletion has been requested. */
  scheduled_deletion_at?: string;
  /**
   * Still on a seeded credential. The UI must show a loud banner —
   * leaving this on a publicly reachable box is a takeover.
   */
  password_is_default?: boolean;
  /**
   * A platform admin made the user a listing reviewer: they review what
   * organisations put up for sale (not their own organisations').
   */
  listing_reviewer?: boolean;
  /** Mirrors the instance-wide ZV_MCP_ENABLED gate. */
  mcp_enabled?: boolean;
  pricing?: unknown;
}

export type Role = "admin" | "org_admin" | "org_member";

/** True for the global admin, who sits outside any organisation. */
export function isGlobalAdmin(u: Pick<Whoami, "role">): boolean {
  return u.role === "admin";
}

/** May author and edit adventures: an organisation's admins. The global admin curates, not authors. */
export function canAuthor(u: Pick<Whoami, "role">): boolean {
  return u.role === "org_admin";
}

export interface LoginRequest {
  username: string;
  password: string;
}

/**
 * Login either completes, or reports that a second factor is needed.
 * The discriminator is `totp_required`; when set, the caller must post
 * the code to /api/auth/login/totp to finish.
 */
/**
 * internal/auth/handlers.go Login. A second factor answers 200 with
 * code "totp_required" and a challenge to send back with the code.
 */
export interface LoginResponse {
  code?: "totp_required";
  challenge?: string;
}

/** internal/auth: signup-config. */
export interface SignupConfig {
  enabled: boolean;
  sso?: { enabled: boolean; button_label?: string; hosted_domain?: string; login_url?: string; provider_name?: string };
}

export interface VersionResponse {
  version: string;
  commit?: string;
  time?: string;
  modified?: boolean;
  go?: string;
  os?: string;
  arch?: string;
}
