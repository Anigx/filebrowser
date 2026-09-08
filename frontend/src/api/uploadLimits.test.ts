import { describe, expect, it } from "vitest";
import {
  CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES,
  DEFAULT_TUS_CHUNK_BYTES,
  effectiveTusChunkSize,
  requiresChunkedUpload,
} from "./uploadLimits";

describe("Cloudflare upload limits", () => {
  it("retains safe configured chunks and defaults invalid values", () => {
    expect(effectiveTusChunkSize(10 * 1024 * 1024)).toBe(10 * 1024 * 1024);
    expect(effectiveTusChunkSize()).toBe(DEFAULT_TUS_CHUNK_BYTES);
    expect(effectiveTusChunkSize(0)).toBe(DEFAULT_TUS_CHUNK_BYTES);
  });

  it("clamps configured chunks below Cloudflare's request cap", () => {
    expect(effectiveTusChunkSize(100 * 1024 * 1024)).toBe(
      CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES
    );
    expect(effectiveTusChunkSize(200 * 1024 * 1024)).toBe(
      CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES
    );
  });

  it("requires resumable upload only beyond the safe direct-request cap", () => {
    expect(
      requiresChunkedUpload(new Blob([new Uint8Array(CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES)]))
    ).toBe(false);
    expect(
      requiresChunkedUpload(
        new Blob([new Uint8Array(CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES + 1)])
      )
    ).toBe(true);
  });
});
