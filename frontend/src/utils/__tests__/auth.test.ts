import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { store, push } = vi.hoisted(() => ({
  store: {
    jwt: "",
    user: null as unknown,
    logoutTimer: null as (() => void) | null,
    setUser(user: unknown) {
      this.user = user;
    },
    setLogoutTimer(timer: () => void) {
      this.logoutTimer = timer;
    },
    clearUser() {
      this.jwt = "";
      this.user = null;
      this.logoutTimer = null;
    },
  },
  push: vi.fn(),
}));
vi.mock("@/stores/auth", () => ({ useAuthStore: () => store }));
vi.mock("@/router", () => ({ default: { push } }));
vi.mock("@/utils/constants", () => ({
  baseURL: "",
  noAuth: false,
  logoutPage: "/login",
  authMethod: "json",
}));
const token = (id: string) =>
  `e30.${btoa(JSON.stringify({ exp: Date.now() / 1000 + 200000, user: { id } }))}.sig`;
let auth: typeof import("../auth");
beforeEach(async () => {
  vi.resetModules();
  vi.useFakeTimers();
  store.clearUser();
  const storage = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
  });
  vi.stubGlobal("document", { cookie: "" });
  vi.stubGlobal("window", {
    setTimeout,
    addEventListener: vi.fn(),
    location: { reload: vi.fn() },
  });
  vi.stubGlobal("navigator", {});
  vi.stubGlobal("fetch", vi.fn());
  auth = await import("../auth");
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
describe("session integration", () => {
  it("adopts another tab's rotated token when an old expiry timer fires before storage delivery", async () => {
    auth.parseToken(token("old"));
    await vi.advanceTimersByTimeAsync(100000000);
    const next = token("next");
    localStorage.setItem("jwt", next);
    await vi.advanceTimersByTimeAsync(100000000);
    expect(store.jwt).toBe(next);
    expect(fetch).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(1);
  });
  it("binds API renewal hints to the token actually sent, not the latest store token", async () => {
    const { fetchURL } = await import("@/api/utils");
    const old = token("old"),
      next = token("next");
    auth.parseToken(old);
    vi.mocked(fetch).mockImplementation(async () => {
      auth.parseToken(next);
      return new Response("ok", { headers: { "X-Renew-Token": "true" } });
    });
    await fetchURL("/api/resources", {});
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledWith(
      "/api/resources",
      expect.objectContaining({ headers: { "X-Auth": old } })
    );
    expect(store.jwt).toBe(next);
  });
  it("does not log out a replacement session on a stale API 401", async () => {
    const { fetchURL } = await import("@/api/utils");
    auth.parseToken(token("old"));
    const next = token("next");
    vi.mocked(fetch).mockImplementation(async () => {
      auth.parseToken(next);
      return new Response("unauthorized", { status: 401 });
    });
    await expect(fetchURL("/api/resources", {})).rejects.toMatchObject({
      status: 401,
    });
    expect(store.jwt).toBe(next);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("serializes independent tab renewals with Web Locks and adopts the rotated token", async () => {
    let queue = Promise.resolve();
    const request = vi.fn((_name: string, action: () => Promise<void>) => {
      const task = queue.then(action);
      queue = task.catch(() => undefined);
      return task;
    });
    vi.stubGlobal("navigator", { locks: { request } });
    const old = token("old"),
      next = token("next");
    auth.parseToken(old);
    vi.resetModules();
    const otherTab = await import("../auth");
    vi.mocked(fetch).mockImplementation(async () => new Response(next));
    await Promise.all([auth.renew(old), otherTab.renew(old)]);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledTimes(2);
    expect(store.jwt).toBe(next);
  });
  it("propagates storage logout and token changes without writing them back", () => {
    auth.parseToken(token("old"));
    const listener = vi.mocked(window.addEventListener).mock
      .calls[0][1] as unknown as (event: { key: string }) => void;
    const next = token("next");
    localStorage.setItem("jwt", next);
    listener({ key: "jwt" });
    expect(store.jwt).toBe(next);
    localStorage.setItem("jwt", "");
    listener({ key: "jwt" });
    expect(store.jwt).toBe("");
    expect(vi.getTimerCount()).toBe(0);
    expect(push).toHaveBeenCalledWith({ path: "/login" });
    expect(fetch).not.toHaveBeenCalled();
  });
  it("waits for renewal before returning request credentials", async () => {
    auth.parseToken(token("old"));
    const next = token("next");
    let finish!: (value: Response) => void;
    vi.mocked(fetch).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      })
    );
    const renewing = auth.renew(store.jwt);
    const credential = auth.getCurrentToken();
    finish(new Response(next));
    await renewing;
    expect(await credential).toBe(next);
  });
  it("clears local state even when backend revocation is unavailable", async () => {
    auth.parseToken(token("old"));
    vi.spyOn(console, "warn").mockImplementation(() => undefined);
    vi.mocked(fetch).mockRejectedValue(new TypeError("offline"));
    await auth.logout();
    expect(store.jwt).toBe("");
    expect(vi.getTimerCount()).toBe(0);
    expect(push).toHaveBeenCalledWith({ path: "/login" });
    expect(console.warn).toHaveBeenCalled();
  });
  it("revokes the current token on logout and cancels even a chained expiry timer", async () => {
    const jwt = token("a");
    auth.parseToken(jwt);
    await vi.advanceTimersByTimeAsync(86400000);
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    await auth.logout();
    expect(fetch).toHaveBeenCalledWith(
      "/api/logout",
      expect.objectContaining({ method: "POST", headers: { "X-Auth": jwt } })
    );
    expect(store.jwt).toBe("");
    expect(localStorage.getItem("jwt")).toBe("");
    expect(vi.getTimerCount()).toBe(0);
  });
  it("coalesces concurrent renewals and ignores late renewal hints for an old token", async () => {
    const old = token("old"),
      next = token("next");
    auth.parseToken(old);
    vi.mocked(fetch).mockResolvedValue(new Response(next));
    await Promise.all([auth.renew(old), auth.renew(old), auth.renew(old)]);
    await auth.renew(old);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(store.jwt).toBe(next);
  });
  it("does not resurrect a session when logout races renewal", async () => {
    const old = token("old"),
      next = token("next");
    auth.parseToken(old);
    let finish!: (res: Response) => void;
    vi.mocked(fetch).mockImplementation((url) =>
      String(url).endsWith("/renew")
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : Promise.resolve(new Response(null, { status: 204 }))
    );
    const pending = auth.renew(old);
    await Promise.resolve();
    await Promise.resolve();
    const loggingOut = auth.logout();
    finish(new Response(next));
    await Promise.allSettled([pending, loggingOut]);
    expect(store.jwt).toBe("");
    expect(localStorage.getItem("jwt")).toBe("");
    expect(fetch).toHaveBeenCalledWith(
      "/api/logout",
      expect.objectContaining({ headers: { "X-Auth": next } })
    );
  });
});
