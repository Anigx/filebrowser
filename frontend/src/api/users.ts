import { fetchURL, fetchJSON, StatusError } from "./utils";

export async function getAll() {
  return fetchJSON<IUser[]>(`/api/users`, {});
}

export async function get(id: number) {
  return fetchJSON<IUser>(`/api/users/${id}`, {});
}

export async function create(
  user: IUser,
  currentPassword: string,
  proof?: string
) {
  const res = await fetchURL(`/api/users`, {
    method: "POST",
    body: JSON.stringify({
      what: "user",
      which: [],
      current_password: currentPassword,
      proof,
      data: user,
    }),
  });

  if (res.status === 201) {
    return res.headers.get("Location");
  }

  throw new StatusError(await res.text(), res.status);
}

export async function update(
  user: Partial<IUser>,
  which = ["all"],
  currentPassword: string | null = null,
  proof?: string
) {
  await fetchURL(`/api/users/${user.id}`, {
    method: "PUT",
    body: JSON.stringify({
      what: "user",
      which: which,
      ...(currentPassword != null ? { current_password: currentPassword } : {}),
      proof,
      data: user,
    }),
  });
}

export async function remove(
  id: number,
  currentPassword: string | null = null,
  proof?: string
) {
  await fetchURL(`/api/users/${id}`, {
    method: "DELETE",
    body: JSON.stringify({
      ...(currentPassword != null ? { current_password: currentPassword } : {}),
      proof,
    }),
  });
}
