import { test, describe } from "node:test";
import assert from "node:assert/strict";
import { ApiError, NetworkError, csrfToken } from "./http.ts";

/** Minimal document stub — csrfToken only reads document.cookie. */
function withCookie<T>(cookie: string, fn: () => T): T {
  const original = Object.getOwnPropertyDescriptor(globalThis, "document");
  try {
    Object.defineProperty(globalThis, "document", {
      value: { cookie },
      configurable: true,
    });
    return fn();
  } finally {
    if (original) Object.defineProperty(globalThis, "document", original);
    else delete (globalThis as { document?: unknown }).document;
  }
}

describe("csrfToken", () => {
  test("reads the token when it is the only cookie", () => {
    assert.equal(withCookie("zv_csrf=abc123", csrfToken), "abc123");
  });

  test("finds it among other cookies", () => {
    assert.equal(
      withCookie("zv_session=xyz; zv_csrf=abc123; zv-theme=dark", csrfToken),
      "abc123",
    );
  });

  test("tolerates leading whitespace between cookies", () => {
    assert.equal(withCookie("a=1;   zv_csrf=tok", csrfToken), "tok");
  });

  test("percent-decodes the value", () => {
    assert.equal(withCookie("zv_csrf=a%2Fb%2Bc", csrfToken), "a/b+c");
  });

  // Must not match zv_csrf_other or my_zv_csrf — a prefix collision
  // would send the wrong token and every mutation would 403.
  test("does not match a cookie whose name merely contains the key", () => {
    assert.equal(withCookie("my_zv_csrf=nope", csrfToken), "");
    assert.equal(withCookie("zv_csrf_backup=nope", csrfToken), "");
  });

  test("returns empty when absent", () => {
    assert.equal(withCookie("zv_session=xyz", csrfToken), "");
    assert.equal(withCookie("", csrfToken), "");
  });
});

describe("ApiError", () => {
  test("carries status, message and code", () => {
    const e = new ApiError(403, "nope", "forbidden_org");
    assert.equal(e.status, 403);
    assert.equal(e.message, "nope");
    assert.equal(e.code, "forbidden_org");
    assert.equal(e.name, "ApiError");
    assert.ok(e instanceof Error);
  });

  // 401 means "re-authenticate", 403 means "stop". The session gate
  // branches on exactly this, and treating a 403 as a logout would
  // bounce a user out of a page they simply cannot see.
  test("distinguishes unauthorized from forbidden", () => {
    const unauth = new ApiError(401, "login required");
    assert.equal(unauth.isUnauthorized, true);
    assert.equal(unauth.isForbidden, false);

    const forbidden = new ApiError(403, "not your org");
    assert.equal(forbidden.isUnauthorized, false);
    assert.equal(forbidden.isForbidden, true);

    const server = new ApiError(500, "boom");
    assert.equal(server.isUnauthorized, false);
    assert.equal(server.isForbidden, false);
  });

  test("code is undefined when the handler emits none", () => {
    assert.equal(new ApiError(400, "bad").code, undefined);
  });
});

describe("NetworkError", () => {
  test("preserves the underlying cause", () => {
    const cause = new TypeError("failed to fetch");
    const e = new NetworkError(cause);
    assert.equal(e.name, "NetworkError");
    assert.equal(e.cause, cause);
    assert.ok(e instanceof Error);
  });
});
