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
  /** Nobody else is in the org, so deleting the account deletes it. */
  sole_member?: boolean;
  /** Its listings go public without review. */
  verified_creator?: boolean;
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
   * An organisation of the user's, or the admin switch, requires 2FA and
   * they have none: the server refuses everything but the setup.
   */
  totp_setup_required?: boolean;
  /** An admin chose the current password; the user is asked to pick their own. */
  password_set_by_admin?: boolean;
  /** The console language the user last chose, absent until they choose one. */
  language?: string;
  /** Organisations waiting for the user to accept or decline joining them. */
  org_invitations?: { org_id: number; org_name: string; role: string }[];
  /**
   * A platform admin made the user a listing reviewer: they review what
   * organisations put up for sale (not their own organisations').
   */
  listing_reviewer?: boolean;
  /** Mirrors the instance-wide ZV_MCP_ENABLED gate. */
  mcp_enabled?: boolean;
  /** Writes are paused for maintenance: skip background writes. */
  maintenance?: boolean;
  /** ZV_PUBLIC_URL, sent to the full platform admin only. */
  public_url?: string;
  pricing?: unknown;
  /** The name the console greets them by, absent while they have given none. */
  display_name?: string;
  /** The welcome dialog was shown to them, on any device. */
  welcome_seen?: boolean;
}

/** "support" is a global admin with the Support role: reads the admin console, changes almost nothing. */
export type Role = "admin" | "support" | "org_admin" | "org_member" | "org_writer";

/** True for the global admin, who sits outside any organisation. */
export function isGlobalAdmin(u: Pick<Whoami, "role">): boolean {
  return u.role === "admin";
}

/** Sees the global admin's console: a full admin, or Support (read-only but for a few support actions). */
export function readsAsAdmin(u: Pick<Whoami, "role">): boolean {
  return u.role === "admin" || u.role === "support";
}

/** May author and edit adventures: an organisation's admins and writers. The global admin curates, not authors. */
export function canAuthor(u: Pick<Whoami, "role">): boolean {
  return u.role === "org_admin" || u.role === "org_writer";
}

/** Runs the organisation's events: its admins and event hosts. A writer only authors. */
export function runsEvents(u: Pick<Whoami, "role">): boolean {
  return u.role === "org_admin" || u.role === "org_member";
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
  /** /terms and /privacy are published, so signup asks for consent. */
  legal_pages?: boolean;
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
