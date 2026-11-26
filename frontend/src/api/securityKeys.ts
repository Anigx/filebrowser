import { getCurrentToken, parseToken } from "@/utils/auth";
import { baseURL } from "@/utils/constants";
import {
  createSecurityCredential,
  getSecurityAssertion,
} from "@/utils/webauthn";
import { StatusError } from "./utils";

interface Ceremony<T> {
  id: string;
  publicKey: T;
}
export interface SecurityKey {
  id: string;
  label: string;
  createdAt: string;
}
export interface KeyRemoval {
  keyID?: string;
  currentPassword?: string;
  proof?: string;
  newPassword?: string;
}

async function request(path: string, payload?: object, token = "") {
  const res = await fetch(`${baseURL}/api/webauthn${path}`, {
    method: payload === undefined ? "GET" : "POST",
    headers: {
      ...(payload !== undefined ? { "Content-Type": "application/json" } : {}),
      ...(token ? { "X-Auth": token } : {}),
    },
    body: payload === undefined ? undefined : JSON.stringify(payload),
  });
  if (!res.ok) throw new StatusError(await res.text(), res.status);
  return res;
}

export async function list(userId: number): Promise<{ keys: SecurityKey[] }> {
  return (
    await request(`/users/${userId}`, undefined, await getCurrentToken())
  ).json();
}

export async function login(username: string, recaptcha: string) {
  const response = await request("/login/start", { username, recaptcha });
  const { id, publicKey }: Ceremony<PublicKeyCredentialRequestOptionsJSON> =
    await response.json();
  const credential = await getSecurityAssertion(publicKey);
  const result = await request("/login/finish", { id, credential });
  parseToken(await result.text());
}

export async function verify(
  action: string,
  targetID: number,
  keyID?: string
): Promise<string> {
  const token = await getCurrentToken();
  const response = await request(
    "/verify/start",
    { action, targetID, keyID },
    token
  );
  const { id, publicKey }: Ceremony<PublicKeyCredentialRequestOptionsJSON> =
    await response.json();
  const credential = await getSecurityAssertion(publicKey);
  const finish = await request("/verify/finish", { id, credential }, token);
  return (await finish.json()).proof;
}

export async function enroll(
  label: string,
  currentPassword?: string,
  proof?: string
) {
  const token = await getCurrentToken();
  const response = await request(
    "/register/start",
    { label, currentPassword, proof },
    token
  );
  const { id, publicKey }: Ceremony<PublicKeyCredentialCreationOptionsJSON> =
    await response.json();
  const credential = await createSecurityCredential(publicKey);
  await request("/register/finish", { id, credential }, token);
}

export async function remove(userId: number, payload: KeyRemoval) {
  await request(`/users/${userId}/remove`, payload, await getCurrentToken());
}
