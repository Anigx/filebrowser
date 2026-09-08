"""Verify cold restore of actual Bolt users/sessions and files, isolated locally."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], stderr=subprocess.STDOUT).decode().strip()


def main():
    image = os.environ["QA_IMAGE"]
    name = "fb-restore-" + uuid.uuid4().hex[:12]
    with tempfile.TemporaryDirectory(prefix="fb-restore-") as tmp:
        root = Path(tmp)
        for area in ("source", "restored"):
            for leaf in ("srv", "config", "database"):
                path = root / area / leaf
                path.mkdir(parents=True)
                os.chown(path, 1000, 1000)
        def start(area):
            args = ["run", "-d", "--name", name, "--read-only", "--cap-drop", "ALL",
                    "--security-opt", "no-new-privileges:true", "--memory", "512m",
                    "--pids-limit", "128", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
                    "-p", "127.0.0.1::80"]
            for leaf in ("srv", "config", "database"):
                args += ["--mount", f"type=bind,src={root / area / leaf},dst=/{leaf}"]
            docker(*args, image)
            port = json.loads(docker("inspect", name))[0]["NetworkSettings"]["Ports"]["80/tcp"][0]["HostPort"]
            base = "http://127.0.0.1:" + port
            for _ in range(60):
                try:
                    with urllib.request.urlopen(base + "/health", timeout=2) as r:
                        if r.status == 200:
                            return base
                except (OSError, urllib.error.URLError):
                    time.sleep(.25)
            raise AssertionError("restored service did not become ready")
        def call(base, path, data=None, token=None):
            h = {"Content-Type": "application/json"}
            if token:
                h["X-Auth"] = token
            with urllib.request.urlopen(urllib.request.Request(base + path, data=data, headers=h), timeout=10) as r:
                return r.status, r.read()
        try:
            base = start("source")
            pw = re.search(r"randomly generated password: (\S+)", docker("logs", name)).group(1)
            _, token = call(base, "/api/login", json.dumps({"username": "admin", "password": pw}).encode())
            data = os.urandom(65536)
            file = root / "source/srv/restore-check.bin"
            file.write_bytes(data)
            os.chown(file, 1000, 1000)
            docker("stop", name)
            docker("rm", "-v", name)
            subprocess.run(["tar", "-czf", str(root / "backup.tar.gz"), "-C", str(root / "source"), "srv", "config", "database"], check=True)
            subprocess.run(["tar", "-xzf", str(root / "backup.tar.gz"), "-C", str(root / "restored")], check=True)
            base = start("restored")
            _, actual = call(base, "/api/raw/restore-check.bin", token=token.decode())
            assert hashlib.sha256(actual).digest() == hashlib.sha256(data).digest()
            status, _ = call(base, "/api/login", json.dumps({"username": "admin", "password": pw}).encode())
            assert status == 200
            print("Actual cold tar backup/restore: user login, persisted JWT session, file SHA256: PASS")
        finally:
            subprocess.run(["docker", "rm", "-f", "-v", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
