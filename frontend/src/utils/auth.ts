import { useAuthStore } from "@/stores/auth";
import router from "@/router";
import type { JwtPayload } from "jwt-decode";
import { jwtDecode } from "jwt-decode";
import { authMethod, baseURL, noAuth, logoutPage } from "./constants";
import { StatusError } from "@/api/utils";
import { setSafeTimeout } from "@/api/utils";

// Web Locks serialize rotation across same-origin tabs. Older browsers retain
// in-tab single-flight protection; storage events still propagate logout.
let renewal: Promise<void> | null = null;
async function sessionLock<T>(action: () => Promise<T>): Promise<T> {
  return navigator.locks
    ? navigator.locks.request(`filebrowser-auth:${baseURL}`, action)
    : action();
}

export function parseToken(token: string, persist = true) {
  // falsy or malformed jwt will throw InvalidTokenError
  const data = jwtDecode<JwtPayload & { user: IUser }>(token);

  document.cookie = `auth=${token}; Path=/; SameSite=Strict;`;

  if (persist) localStorage.setItem("jwt", token);

  const authStore = useAuthStore();
  authStore.jwt = token;
  authStore.setUser(data.user);

  authStore.logoutTimer?.();
  authStore.setLogoutTimer(null);

  // proxy auth with custom logout subject to unknown external timeout
  if (logoutPage !== "/login" && authMethod === "proxy") {
    console.warn("idle timeout disabled with proxy auth and custom logout");
    return;
  }

  const expiresAt = new Date(data.exp! * 1000);
  const timeout = expiresAt.getTime() - Date.now();
  authStore.setLogoutTimer(
    setSafeTimeout(() => {
      // A suspended tab can run its old timer before its storage event.
      const current = localStorage.getItem("jwt");
      if (current && current !== token) {
        parseToken(current, false);
      } else {
        logout("inactivity");
      }
    }, timeout)
  );
}

export async function validateLogin() {
  try {
    if (localStorage.getItem("jwt")) {
      await renew(<string>localStorage.getItem("jwt"));
    }
  } catch (error) {
    console.warn("Invalid JWT token in storage");
    throw error;
  }
}

export async function login(
  username: string,
  password: string,
  recaptcha: string
) {
  const data = { username, password, recaptcha };

  const res = await fetch(`${baseURL}/api/login`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(data),
  });

  const body = await res.text();

  if (res.status === 200) {
    parseToken(body);
  } else {
    throw new StatusError(
      body || `${res.status} ${res.statusText}`,
      res.status
    );
  }
}

export function renew(jwt: string): Promise<void> {
  if (renewal) return renewal;
  renewal = sessionLock(async () => {
    const current = localStorage.getItem("jwt");
    if (!current) return;
    // A different tab (or an earlier response) already rotated this token.
    if (current !== jwt) {
      parseToken(current, false);
      return;
    }
    const res = await fetch(`${baseURL}/api/renew`, {
      method: "POST",
      headers: { "X-Auth": current },
    });
    const body = await res.text();
    if (res.status !== 200) {
      throw new StatusError(
        body || `${res.status} ${res.statusText}`,
        res.status
      );
    }
    if (localStorage.getItem("jwt") !== current) {
      // Logout/login won the race. Never restore the old session, and revoke
      // the replacement credential that the server just minted.
      await revokeToken(body);
      return;
    }
    parseToken(body);
  }).finally(() => {
    renewal = null;
  });
  return renewal;
}

export async function getCurrentToken(): Promise<string> {
  if (renewal) await renewal;
  return sessionLock(async () => {
    const token = localStorage.getItem("jwt") || "";
    if (token && token !== useAuthStore().jwt) parseToken(token, false);
    return token;
  });
}

async function revokeToken(jwt: string) {
  if (!jwt) return;
  const res = await fetch(`${baseURL}/api/logout`, {
    method: "POST",
    headers: { "X-Auth": jwt },
    keepalive: true,
  });
  if (!res.ok && res.status !== 401) {
    throw new StatusError("Session revocation failed", res.status);
  }
}

export async function signup(username: string, password: string) {
  const data = { username, password };

  const res = await fetch(`${baseURL}/api/signup`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(data),
  });

  if (res.status !== 200) {
    const body = await res.text();
    throw new StatusError(
      body || `${res.status} ${res.statusText}`,
      res.status
    );
  }
}

function clearSession() {
  const authStore = useAuthStore();
  authStore.logoutTimer?.();
  authStore.clearUser();
  document.cookie = "auth=; Max-Age=0; Path=/; SameSite=Strict;";
}

window.addEventListener("storage", (event) => {
  if (event.key !== "jwt" && event.key !== null) return;
  const token = localStorage.getItem("jwt");
  if (token) {
    try {
      parseToken(token, false);
    } catch {
      clearSession();
    }
  } else {
    clearSession();
    router.push({ path: "/login" });
  }
});

export async function logout(reason?: string) {
  const jwt = localStorage.getItem("jwt") || useAuthStore().jwt;
  clearSession();
  localStorage.setItem("jwt", "");
  try {
    // Allow an in-flight renewal to revoke its replacement before navigating.
    if (renewal) await renewal.catch(() => undefined);
    await sessionLock(() => revokeToken(jwt));
  } catch (error) {
    // Local logout must complete even offline; do not claim server revocation.
    console.warn("Could not revoke session on server", error);
  }
  // A new login in this or another tab should not be redirected by an old logout.
  if (localStorage.getItem("jwt")) return;

  if (noAuth) {
    window.location.reload();
  } else if (logoutPage !== "/login") {
    document.location.href = `${logoutPage}`;
  } else {
    if (typeof reason === "string" && reason.trim() !== "") {
      router.push({
        path: "/login",
        query: { "logout-reason": reason },
      });
    } else {
      router.push({
        path: "/login",
      });
    }
  }
}
