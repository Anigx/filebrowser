// Keep every request comfortably below Cloudflare's 100 MB proxied-request cap.
// The margin accounts for request framing and avoids relying on plan-specific
// boundary behavior.
export const CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES = 95 * 1024 * 1024;
export const DEFAULT_TUS_CHUNK_BYTES = 10 * 1024 * 1024;

export function effectiveTusChunkSize(configured?: number): number {
  if (!Number.isFinite(configured) || !configured || configured <= 0) {
    return DEFAULT_TUS_CHUNK_BYTES;
  }

  return Math.min(Math.floor(configured), CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES);
}

export function requiresChunkedUpload(content: ApiContent): boolean {
  return (
    content instanceof Blob &&
    content.size > CLOUDFLARE_SAFE_UPLOAD_REQUEST_BYTES
  );
}
