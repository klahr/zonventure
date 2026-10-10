/**
 * WebAuthn in the browser, for the passkeys listing reviewers sign their
 * decisions with (internal/admin/reviewer_passkeys.go). The server sends
 * go-webauthn's options as JSON, binary fields base64url; the browser
 * wants ArrayBuffers, and the answer goes back as JSON again. Browsers
 * with PublicKeyCredential.parse…FromJSON and toJSON() use those; the
 * rest get the same conversion by hand.
 */

type Json = Record<string, unknown>;

/** base64url (padded or not) to bytes. */
export function fromB64url(s: string): ArrayBuffer {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/").replace(/=+$/, "");
  const bin = atob(b64 + "===".slice((b64.length + 3) % 4));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

/** Bytes to unpadded base64url. */
export function toB64url(buf: ArrayBuffer | ArrayBufferView): string {
  const bytes = buf instanceof ArrayBuffer ? new Uint8Array(buf) : new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** The options' publicKey, whether wrapped ({publicKey}) or not. */
function publicKeyOf(options: Json): Json {
  return (options.publicKey as Json | undefined) ?? options;
}

const withIds = (list: unknown) =>
  Array.isArray(list) ? list.map((c: Json) => ({ ...c, id: fromB64url(c.id as string) })) : undefined;

/** Creation options from the server's JSON, binary fields decoded. */
export function creationOptions(options: Json): PublicKeyCredentialCreationOptions {
  const pk = publicKeyOf(options);
  const user = pk.user as Json;
  const out: Json = { ...pk, challenge: fromB64url(pk.challenge as string), user: { ...user, id: fromB64url(user.id as string) } };
  const exclude = withIds(pk.excludeCredentials);
  if (exclude) out.excludeCredentials = exclude;
  return out as unknown as PublicKeyCredentialCreationOptions;
}

/** Request (assertion) options from the server's JSON, binary fields decoded. */
export function requestOptions(options: Json): PublicKeyCredentialRequestOptions {
  const pk = publicKeyOf(options);
  const out: Json = { ...pk, challenge: fromB64url(pk.challenge as string) };
  const allow = withIds(pk.allowCredentials);
  if (allow) out.allowCredentials = allow;
  return out as unknown as PublicKeyCredentialRequestOptions;
}

/** A credential the browser returned, as JSON for the server. */
export function credentialJSON(cred: PublicKeyCredential): Json {
  const withJSON = cred as PublicKeyCredential & { toJSON?: () => Json };
  if (typeof withJSON.toJSON === "function") return withJSON.toJSON();
  const r = cred.response as AuthenticatorResponse & Partial<AuthenticatorAttestationResponse & AuthenticatorAssertionResponse>;
  const response: Json = { clientDataJSON: toB64url(r.clientDataJSON) };
  if (r.attestationObject) {
    response.attestationObject = toB64url(r.attestationObject);
    if (typeof r.getTransports === "function") response.transports = r.getTransports();
  }
  if (r.authenticatorData) response.authenticatorData = toB64url(r.authenticatorData);
  if (r.signature) response.signature = toB64url(r.signature);
  if (r.userHandle) response.userHandle = toB64url(r.userHandle);
  return {
    id: cred.id,
    rawId: toB64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    response,
    clientExtensionResults: cred.getClientExtensionResults?.() ?? {},
  };
}

type ParseStatics = {
  parseCreationOptionsFromJSON?: (o: Json) => PublicKeyCredentialCreationOptions;
  parseRequestOptionsFromJSON?: (o: Json) => PublicKeyCredentialRequestOptions;
};

/** Whether this browser can make and use passkeys at all. */
export function passkeysSupported(): boolean {
  return typeof window !== "undefined" && typeof window.PublicKeyCredential === "function" && !!navigator.credentials;
}

/** Makes a passkey from the server's creation options; its JSON for the server. */
export async function createPasskey(options: Json): Promise<Json> {
  const statics = window.PublicKeyCredential as unknown as ParseStatics;
  const publicKey = statics.parseCreationOptionsFromJSON
    ? statics.parseCreationOptionsFromJSON(publicKeyOf(options))
    : creationOptions(options);
  const cred = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("No passkey was made", "NotAllowedError");
  return credentialJSON(cred);
}

/** Signs the server's challenge with a passkey; the assertion's JSON for the server. */
export async function signWithPasskey(options: Json): Promise<Json> {
  const statics = window.PublicKeyCredential as unknown as ParseStatics;
  const publicKey = statics.parseRequestOptionsFromJSON
    ? statics.parseRequestOptionsFromJSON(publicKeyOf(options))
    : requestOptions(options);
  const cred = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("Nothing was signed", "NotAllowedError");
  return credentialJSON(cred);
}

/** The person closed or refused the browser's passkey prompt. */
export function isCancelled(e: unknown): boolean {
  return e instanceof DOMException && (e.name === "NotAllowedError" || e.name === "AbortError");
}
