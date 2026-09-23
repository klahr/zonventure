// HTTP layer for the JSON API the Go server already exposes.
//
// Deliberately thin. The server is API-first (263 chi routes, a real
// JSON API under /api/{auth,admin,player}) and the existing HTML pages
// consume it as pure clients, so there is nothing to abstract over —
// only cookies, CSRF, and error shape to get right once.

/** The server's error body: httpx.Error writes {"error": "..."}. */
interface ErrorBody {
  error?: string;
  code?: string;
}

/**
 * A non-2xx response.
 *
 * `code` is a stable machine-readable identifier when the handler
 * emits one; callers should branch on it rather than on `message`,
 * which is English prose and is not a contract. Handlers that do not
 * emit a code yet leave it undefined — that is the documented
 * incremental upgrade path, not an omission to work around.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string | undefined;
  /**
   * The decoded error body, when it was JSON. Some refusals carry more
   * than a message — a rejected graph save includes the full validation
   * report — and the caller needs that to show every problem at once.
   */
  readonly body: unknown;

  constructor(status: number, message: string, code?: string, body?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.body = body;
  }

  /** 401. The session is gone — re-authenticate rather than retrying. */
  get isUnauthorized(): boolean {
    return this.status === 401;
  }

  /** 403. Authenticated but not permitted; retrying will not help. */
  get isForbidden(): boolean {
    return this.status === 403;
  }
}

/** The network never answered — offline, DNS, TLS, or an aborted fetch. */
export class NetworkError extends Error {
  constructor(cause: unknown) {
    super("network request failed");
    this.name = "NetworkError";
    this.cause = cause;
  }
}

const CSRF_COOKIE = "zv_csrf";
const CSRF_HEADER = "X-CSRF-Token";

/**
 * Read the CSRF token the server seeded.
 *
 * The auth.EnsureCSRF middleware mints `zv_csrf` on every response and
 * deliberately leaves it non-HttpOnly so the front-end can read it —
 * that is the design, not an oversight. The token outlives the session
 * (30 days vs 7), so the first POST after a logout-then-login still
 * finds a usable one without an extra round trip.
 */
export function csrfToken(): string {
  const prefix = CSRF_COOKIE + "=";
  for (const part of document.cookie.split(";")) {
    const c = part.trim();
    if (c.startsWith(prefix)) return decodeURIComponent(c.slice(prefix.length));
  }
  return "";
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
  headers?: Record<string, string>;
  /** A multipart body. The browser sets its Content-Type. */
  form?: FormData;
}

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

/**
 * Issue a request and decode the JSON response.
 *
 * Cookies are sent same-origin (both the staff `zv_session` and the
 * player `zv_slot` domains are cookie-based), and mutating verbs carry
 * the CSRF header.
 */
export async function request<T>(
  path: string,
  opts: RequestOptions = {},
): Promise<T> {
  const method = (opts.method ?? "GET").toUpperCase();
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...opts.headers,
  };

  if (!SAFE_METHODS.has(method)) {
    const token = csrfToken();
    if (token) headers[CSRF_HEADER] = token;
  }
  if (opts.body !== undefined && !opts.form) {
    headers["Content-Type"] = "application/json";
  }

  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers,
      ...(opts.form
        ? { body: opts.form }
        : opts.body !== undefined
          ? { body: JSON.stringify(opts.body) }
          : {}),
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
  } catch (e) {
    // Re-throw aborts untouched: an abort is the caller cancelling,
    // not a failure, and callers check signal.aborted to ignore it.
    if (e instanceof DOMException && e.name === "AbortError") throw e;
    throw new NetworkError(e);
  }

  if (!res.ok) throw await errorFrom(res);

  // 204, and any empty body, decode to undefined rather than blowing
  // up in JSON.parse. Callers type those as void.
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  if (text === "") return undefined as T;
  return JSON.parse(text) as T;
}

async function errorFrom(res: Response): Promise<ApiError> {
  let message = `HTTP ${res.status}`;
  let code: string | undefined;
  let body: unknown;
  try {
    body = await res.json();
    const b = body as ErrorBody;
    if (b.error) message = b.error;
    if (b.code) code = b.code;
  } catch {
    // Not JSON — a proxy error page, or an empty body. Keep the
    // status-derived message rather than surfacing a parse failure,
    // which would tell the user nothing useful.
  }
  return new ApiError(res.status, message, code, body);
}

export const get = <T>(path: string, opts?: RequestOptions) =>
  request<T>(path, { ...opts, method: "GET" });

export const post = <T>(path: string, body?: unknown, opts?: RequestOptions) =>
  request<T>(path, {
    ...opts,
    method: "POST",
    ...(body !== undefined ? { body } : {}),
  });

export const put = <T>(path: string, body?: unknown, opts?: RequestOptions) =>
  request<T>(path, {
    ...opts,
    method: "PUT",
    ...(body !== undefined ? { body } : {}),
  });

export const patch = <T>(path: string, body?: unknown, opts?: RequestOptions) =>
  request<T>(path, {
    ...opts,
    method: "PATCH",
    ...(body !== undefined ? { body } : {}),
  });
