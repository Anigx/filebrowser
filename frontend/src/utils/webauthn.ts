export function decodeKeyBytes(value: string): ArrayBuffer {
  const text = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(text, (c) => c.charCodeAt(0)).buffer;
}

export function encodeKeyBytes(value: ArrayBuffer): string {
  return btoa(
    Array.from(new Uint8Array(value), (c) => String.fromCharCode(c)).join("")
  )
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

export function supportsSecurityKeys(): boolean {
  return (
    typeof window !== "undefined" &&
    window.isSecureContext &&
    typeof PublicKeyCredential !== "undefined" &&
    typeof navigator.credentials?.create === "function" &&
    typeof navigator.credentials?.get === "function"
  );
}

export function isSecurityKeyCancellation(err: unknown): boolean {
  return (
    err instanceof DOMException &&
    ["NotAllowedError", "AbortError"].includes(err.name)
  );
}

export async function createSecurityCredential(
  options: PublicKeyCredentialCreationOptionsJSON
): Promise<object> {
  const publicKey =
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === "function"
      ? PublicKeyCredential.parseCreationOptionsFromJSON(options)
      : ({
          ...options,
          challenge: decodeKeyBytes(options.challenge),
          user: { ...options.user, id: decodeKeyBytes(options.user.id) },
          excludeCredentials: options.excludeCredentials?.map((item) => ({
            ...item,
            id: decodeKeyBytes(item.id),
          })),
        } as PublicKeyCredentialCreationOptions);
  const credential = await navigator.credentials.create({ publicKey });
  if (!(credential instanceof PublicKeyCredential))
    throw new DOMException("", "NotAllowedError");
  if (typeof credential.toJSON === "function") return credential.toJSON();
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: encodeKeyBytes(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      clientDataJSON: encodeKeyBytes(response.clientDataJSON),
      attestationObject: encodeKeyBytes(response.attestationObject),
      transports: response.getTransports?.() || [],
    },
  };
}

export async function getSecurityAssertion(
  options: PublicKeyCredentialRequestOptionsJSON
): Promise<object> {
  const publicKey =
    typeof PublicKeyCredential.parseRequestOptionsFromJSON === "function"
      ? PublicKeyCredential.parseRequestOptionsFromJSON(options)
      : ({
          ...options,
          challenge: decodeKeyBytes(options.challenge),
          allowCredentials: options.allowCredentials?.map((item) => ({
            ...item,
            id: decodeKeyBytes(item.id),
          })),
        } as PublicKeyCredentialRequestOptions);
  const credential = await navigator.credentials.get({ publicKey });
  if (!(credential instanceof PublicKeyCredential))
    throw new DOMException("", "NotAllowedError");
  if (typeof credential.toJSON === "function") return credential.toJSON();
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: encodeKeyBytes(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      clientDataJSON: encodeKeyBytes(response.clientDataJSON),
      authenticatorData: encodeKeyBytes(response.authenticatorData),
      signature: encodeKeyBytes(response.signature),
      userHandle: response.userHandle
        ? encodeKeyBytes(response.userHandle)
        : null,
    },
  };
}
