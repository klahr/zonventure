import { test } from "node:test";
import assert from "node:assert/strict";
import { creationOptions, fromB64url, requestOptions, toB64url } from "./ui/webauthn.ts";

// go-webauthn sends binary fields as base64url; the browser wants bytes.
test("base64url round-trips bytes, padded or not", () => {
  const bytes = new Uint8Array([0, 1, 250, 251, 252, 253, 254, 255, 62, 63]);
  const s = toB64url(bytes.buffer);
  assert.doesNotMatch(s, /[+/=]/);
  assert.deepEqual(new Uint8Array(fromB64url(s)), bytes);
  assert.deepEqual(new Uint8Array(fromB64url(s + "==")), bytes);
});

test("creation options decode the challenge, the user id and the exclusions", () => {
  const challenge = toB64url(new Uint8Array([1, 2, 3]).buffer);
  const o = creationOptions({
    publicKey: {
      challenge,
      rp: { id: "zv.example.com", name: "Zonventure" },
      user: { id: toB64url(new Uint8Array([9, 9]).buffer), name: "root", displayName: "root" },
      excludeCredentials: [{ type: "public-key", id: toB64url(new Uint8Array([7]).buffer) }],
    },
  });
  assert.deepEqual(new Uint8Array(o.challenge as ArrayBuffer), new Uint8Array([1, 2, 3]));
  assert.deepEqual(new Uint8Array(o.user.id as ArrayBuffer), new Uint8Array([9, 9]));
  assert.deepEqual(new Uint8Array(o.excludeCredentials![0]!.id as ArrayBuffer), new Uint8Array([7]));
  assert.equal(o.rp.id, "zv.example.com");
});

test("request options decode the challenge and the allowed credentials", () => {
  const o = requestOptions({
    publicKey: {
      challenge: toB64url(new Uint8Array([4, 5]).buffer),
      allowCredentials: [{ type: "public-key", id: toB64url(new Uint8Array([8]).buffer) }],
      userVerification: "required",
    },
  });
  assert.deepEqual(new Uint8Array(o.challenge as ArrayBuffer), new Uint8Array([4, 5]));
  assert.deepEqual(new Uint8Array(o.allowCredentials![0]!.id as ArrayBuffer), new Uint8Array([8]));
  assert.equal(o.userVerification, "required");
});
