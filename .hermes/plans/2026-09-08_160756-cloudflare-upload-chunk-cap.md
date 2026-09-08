# Cloudflare-safe upload chunk cap Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Guarantee that browser upload requests routed through the File Browser UI stay below Cloudflare's 100 MB per-request limit, while retaining resumable TUS uploads and direct-upload compatibility for small files.

**Architecture:** The UI already uses `tus-js-client` with `chunkSize` from `TusSettings` (currently generated as 10 MiB in `frontend/index.html`). Preserve TUS as the resumable transport and introduce a shared Cloudflare-safe effective-chunk calculation in the frontend: clamp configured TUS chunks to a conservative 95 MiB ceiling, and force TUS for any browser `Blob` that could exceed that ceiling. Server-side TUS PATCH handling gets a matching configurable/default hard cap so a malformed or custom client cannot route an oversized PATCH through the proxy.

**Tech Stack:** Vue/TypeScript, `tus-js-client`, Go HTTP handlers, Vitest/frontend tests, Go HTTP regression tests, Docker Go 1.25.

---

## Current evidence

- `frontend/index.html:51` exposes `TusSettings: { chunkSize: 10485760, retryCount: 5 }`; the standard browser UI therefore already produces 10 MiB TUS PATCH requests.
- `frontend/src/api/tus.ts:31-35` passes that setting directly to `tus.Upload`.
- `frontend/src/api/files.ts:106-124` selects TUS only when the browser and input support it; the fallback uses the single-request `/api/resources` path.
- `http/tus_handlers.go` accepts PATCH bodies but currently has no explicit Cloudflare-oriented per-PATCH content-length ceiling.

## Design decisions

1. Use **95 MiB** (`99_614_720` bytes) as the wire-safe application limit, rather than exactly 100 MB. This reserves space for HTTP framing and makes the policy robust if Cloudflare's documented limit is enforced slightly differently by plan/proxy mode.
2. Clamp any configured TUS chunk to 95 MiB, but retain smaller configured chunks such as the current 10 MiB default.
3. Do not attempt to split ordinary `/api/resources` request streams server-side: HTTP cannot retroactively turn one request into multiple proxy requests. The browser must select TUS before sending a large file.
4. If TUS is unavailable and a `Blob` is larger than the cap, reject before transmission with a clear client error. Do not send a request Cloudflare will reject.
5. Server-side, reject oversized TUS PATCH `Content-Length` values and limit unknown-length chunk bodies to cap + 1. This also protects custom clients and direct origins.

## Task 1: Add a shared frontend policy module

**Objective:** Provide one authoritative effective TUS chunk size and large-upload eligibility decision.

**Files:**
- Create: `frontend/src/api/uploadLimits.ts`
- Test: `frontend/src/api/uploadLimits.test.ts`

**Step 1: Write failing tests**

Cover:
- a 10 MiB configured chunk remains 10 MiB;
- an empty/missing config uses a safe default (10 MiB);
- `100 MiB` and `200 MiB` configured chunks clamp to 95 MiB;
- a 95 MiB blob is eligible for TUS;
- a blob larger than 95 MiB is never sent through the resource fallback.

**Step 2: Run the targeted frontend test**

Run: `pnpm --dir frontend test uploadLimits`

Expected before implementation: failure because the module does not exist.

**Step 3: Implement the minimum policy**

Export immutable constants and pure helpers:

```ts
export const CLOUDFLARE_MAX_UPLOAD_REQUEST_BYTES = 95 * 1024 * 1024;
export const DEFAULT_TUS_CHUNK_BYTES = 10 * 1024 * 1024;
export function effectiveTusChunkSize(configured?: number): number;
export function requiresChunkedUpload(content: ApiContent): boolean;
```

**Step 4: Re-run targeted frontend test**

Expected: PASS.

## Task 2: Apply the policy to browser transport selection

**Objective:** Ensure the UI cannot send a large browser file using one `/api/resources` request.

**Files:**
- Modify: `frontend/src/api/tus.ts`
- Modify: `frontend/src/api/files.ts`
- Test: `frontend/src/api/files.test.ts` or the repository's closest existing API test location

**Step 1: Write failing transport-selection tests**

Mock TUS support and `postResources`/TUS upload dispatch. Verify:
- supported `Blob` upload uses TUS with an effective chunk <= 95 MiB;
- unsupported-TUS `Blob` over 95 MiB fails locally and makes no `/api/resources` request;
- small unsupported-TUS blobs retain the legacy resource fallback;
- directory creation remains unchanged.

**Step 2: Implement transport decision**

- In `tus.ts`, feed `effectiveTusChunkSize(tusSettings?.chunkSize)` to `tus.Upload`.
- In `files.ts`, before selecting `postResources`, throw a descriptive error if an oversized Blob requires TUS but TUS is not available.
- Preserve existing retry and overwrite behavior.

**Step 3: Run frontend tests**

Run: `pnpm --dir frontend test`

Expected: PASS.

## Task 3: Enforce the cap on the TUS HTTP endpoint

**Objective:** Prevent custom/browser clients from sending an oversized PATCH directly to File Browser.

**Files:**
- Modify: `http/tus_handlers.go`
- Modify/Create: focused HTTP test beside `http/tus_upload_length_test.go`

**Step 1: Write failing Go regressions**

Test that:
- `PATCH /api/tus/...` with `Content-Length = 95 MiB + 1` returns `413 Request Entity Too Large` before writing;
- an unknown-length body is read through `http.MaxBytesReader`/bounded reader and cannot write more than the cap;
- a chunk exactly at the cap remains accepted when its declared upload length and offset are valid;
- the existing 10 MiB multi-chunk upload remains successful.

**Step 2: Implement minimal handler cap**

- Declare a named `maxTusPatchBytes = 95 << 20` constant (or an explicit settings-backed equivalent if existing configuration conventions permit it).
- Reject declared oversize content before opening/appending the upload file.
- Wrap unknown/chunked request bodies in a byte-limited reader and map limit overflow to `413`.
- Keep the existing TUS object identity and lease protections intact.

**Step 3: Run focused backend tests**

Run:

```sh
docker run --rm --network none -e GOTOOLCHAIN=local \
  -v /tmp/filebrowser-go-cache:/go -v /root/filebrowser:/src:ro \
  -w /src golang:1.25 \
  /usr/local/go/bin/go test ./http -run 'TestTus.*(Chunk|Limit|Cloudflare)' -count=1
```

Expected: PASS.

## Task 4: Build and test the actual browser bundle

**Objective:** Verify that production-delivered frontend settings retain a Cloudflare-safe request size.

**Files:**
- Modify if required: `frontend/index.html`
- Optional test: frontend integration/e2e fixture

**Steps:**

1. Keep the shipped default at 10 MiB unless the user explicitly requests another size.
2. Run:

```sh
pnpm --dir frontend build
```

3. Inspect the generated bundle/config or unit mock to confirm `chunkSize` stays <= 95 MiB.
4. Run the Go embedded-frontend/build process used by this repository, if applicable.

## Task 5: Full verification and documentation

**Objective:** Verify behavior, avoid proxy false confidence, and explain operator-facing behavior.

**Files:**
- Modify: relevant settings UI/help text and/or `frontend/src/i18n/en.json`, `frontend/src/i18n/de.json` if UI copy is changed.
- Optional: README/deployment documentation if this fork maintains it.

**Verification:**

```sh
# Frontend
pnpm --dir frontend test
pnpm --dir frontend build

# Backend, isolated from network after cache warmup
docker run --rm --network none -e GOTOOLCHAIN=local \
  -v /tmp/filebrowser-go-cache:/go -v /root/filebrowser:/src:ro \
  -w /src golang:1.25 /usr/local/go/bin/go test ./... -count=1

docker run --rm --network none -e GOTOOLCHAIN=local \
  -v /tmp/filebrowser-go-cache:/go -v /root/filebrowser:/src:ro \
  -w /src golang:1.25 /usr/local/go/bin/go vet ./...

git diff --check
```

**Manual acceptance scenario:** Upload a synthetic file larger than 100 MiB through the browser behind a test reverse proxy/body-size guard set to 100 MiB. Observe multiple TUS PATCH requests, each <= 95 MiB, successful final file size/hash, and no single oversized `/api/resources` request.

## Risks and compatibility

- Cloudflare limits vary by plan and may differ for proxied versus DNS-only records. The fixed 95 MiB cap is conservative for a stated 100 MB proxy limit, but administrators with lower plan-specific limits need a future configurable policy.
- The direct resource fallback remains available only for blobs <= 95 MiB. Non-browser clients must use the TUS API for larger uploads.
- Proxy limits apply to request bodies; compression/content encoding must not be relied upon to evade the cap.
- This feature cannot make a third-party client automatically chunk an already single HTTP POST; client-side TUS selection is essential.
