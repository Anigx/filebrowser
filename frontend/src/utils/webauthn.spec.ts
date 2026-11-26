import { describe, expect, it } from "vitest";
import {
  decodeKeyBytes,
  encodeKeyBytes,
  supportsSecurityKeys,
} from "./webauthn";

describe("WebAuthn browser support", () => {
  it("round trips URL-safe challenge and credential bytes without padding", () => {
    const bytes = new Uint8Array([255, 238, 0, 126]);
    expect(encodeKeyBytes(bytes.buffer)).toBe("_-4Afg");
    expect(new Uint8Array(decodeKeyBytes("_-4Afg"))).toEqual(bytes);
  });

  it("is false when the browser does not implement WebAuthn", () => {
    expect(supportsSecurityKeys()).toBe(false);
  });
});
