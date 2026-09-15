# Command execution and hooks

> [!CAUTION]
>
> Command execution converts File Browser into an execution broker. It is not a
> normal file-management feature and must remain disabled unless there is a
> separately reviewed operational requirement.

## Safe default in this fork

The supplied Docker image deliberately contains **no execution sandbox launcher**.
All execution paths — interactive commands, event hooks, and authentication hooks
— call the same `runner.NewCommand` boundary. It refuses every command unless a
valid `executionSandbox` setting is enabled. There is no raw `os/exec` fallback.

Consequences for the standard Compose deployment:

- `--disable-exec=false` alone does **not** enable command execution.
- Adding a command in the UI or CLI does **not** enable command execution.
- Enabling hook authentication without an independently provided sandbox causes
  login to fail closed rather than running a host command.
- There is intentionally no `EXECUTION_SANDBOX_*` environment variable in
  Compose. The setting is persistent application state; a misleading runtime
  variable must not appear to configure it.

## When execution is genuinely required

Do not modify the standard image or Compose profile in place. Build a separate,
explicitly named deployment image and assess it as privileged infrastructure.
The external launcher configured through `executionSandbox.command` must:

1. be an absolute executable path and end in `--`; File Browser appends the
   requested executable and its arguments after that delimiter;
2. run the target as an unprivileged UID/GID;
3. disable networking by default;
4. expose only explicit read-only inputs and a minimal writable working area;
5. enforce process-count, wall-clock, CPU, memory, and output limits; and
6. deny capability escalation, device access, host mounts, and setuid paths.

The launcher has to work with the actual container security profile. Namespace
launchers often need kernel/Docker permissions that conflict with
`no-new-privileges`, dropped capabilities, or Docker's seccomp policy. Do not
weaken those controls merely to make a launcher start. If the launcher cannot be
proven to isolate the command in the target environment, keep execution disabled.

The server-side boundary also sets a default 30-second deadline and 1 MiB output
limit. Those are defense in depth, **not** a substitute for OS-level isolation.

## Hooks and command UI

With a separately reviewed sandbox deployment, File Browser supports event hooks
for copy, rename, upload, delete, and save. The interactive command UI remains
limited to commands granted to the user. These controls decide *which* command
may be requested; the external launcher decides whether it can safely execute.

Authentication hooks are especially sensitive because they process credentials.
The hook command receives `USERNAME` and `PASSWORD` via its environment. Treat
that launcher and hook implementation as authentication infrastructure: prevent
logging, network exfiltration, shell parsing of untrusted inputs, and inherited
secrets. Prefer a supported external identity provider over auth hooks.

## Verification gate for a custom execution image

Before enabling execution for users, verify in an isolated test deployment:

```sh
go test ./runner ./auth ./http
# Then run a deliberately harmless command through the real launcher and prove:
# - no network route exists;
# - the process has the intended UID/GID;
# - forbidden files/devices are unavailable;
# - timeout, process, and output limits terminate it;
# - malformed launcher settings fail closed.
```

Record the image digest, launcher version/configuration, kernel/Docker version,
and the result. Re-run this gate whenever any of them changes.

For the fork-wide advisory mapping and residual risks, see
[`security-status.md`](security-status.md).
