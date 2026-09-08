import { beforeEach, expect, it, vi } from "vitest";
import type {
  HttpRequest,
  HttpResponse,
  UploadOptions,
  DetailedError,
} from "tus-js-client";
const mocks = vi.hoisted(() => ({
  options: null as UploadOptions | null,
  getCurrentToken: vi.fn(),
  renew: vi.fn(),
}));
vi.mock("tus-js-client", () => ({
  Upload: class {
    constructor(_content: unknown, options: UploadOptions) {
      mocks.options = options;
    }
    start() {}
  },
}));
vi.mock("@/utils/auth", () => ({
  getCurrentToken: mocks.getCurrentToken,
  renew: mocks.renew,
}));
vi.mock("@/api/utils", () => ({ removePrefix: (path: string) => path }));
vi.mock("@/utils/constants", () => ({
  baseURL: "",
  tusEndpoint: "/api/tus",
  origin: "https://example.test",
  tusSettings: { chunkSize: 1024, retryCount: 3 },
}));
import { upload } from "./tus";
beforeEach(() => vi.clearAllMocks());
it("awaits coordinated credentials for every TUS request and handles renewal hints", async () => {
  const pending = upload("/file", new Blob(["hello"]), false, undefined);
  const options = mocks.options!;
  const headers = new Map<string, string>();
  const request = {
    setHeader: (key: string, value: string) => headers.set(key, value),
    getHeader: (key: string) => headers.get(key),
  } as unknown as HttpRequest;
  let finish!: (value: string) => void;
  mocks.getCurrentToken.mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  const before = options.onBeforeRequest!(request);
  expect(headers.has("X-Auth")).toBe(false);
  finish("first");
  await before;
  expect(headers.get("X-Auth")).toBe("first");
  mocks.renew.mockResolvedValue(undefined);
  await options.onAfterResponse!(request, {
    getHeader: () => "true",
  } as unknown as HttpResponse);
  expect(mocks.renew).toHaveBeenCalledWith("first");
  mocks.getCurrentToken.mockResolvedValue("rotated");
  await options.onBeforeRequest!(request);
  expect(headers.get("X-Auth")).toBe("rotated");
  mocks.getCurrentToken.mockResolvedValue("");
  await expect(options.onBeforeRequest!(request)).rejects.toThrow(
    "Not authenticated"
  );
  options.onSuccess!({} as never);
  await pending;
});
it("does not retry authorization failures or conflicts", async () => {
  const pending = upload("/file", new Blob(["hello"]), false, undefined);
  const options = mocks.options!;
  for (const status of [401, 403, 409]) {
    expect(
      options.onShouldRetry!(
        { originalResponse: { getStatus: () => status } } as DetailedError,
        0,
        options
      )
    ).toBe(false);
  }
  expect(
    options.onShouldRetry!(
      { originalResponse: { getStatus: () => 503 } } as DetailedError,
      0,
      options
    )
  ).toBe(true);
  options.onSuccess!({} as never);
  await pending;
});
