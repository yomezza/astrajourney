#!/usr/bin/env python3
"""N0 only: owns a temporary loopback NATS process, never accepts a live URL/store.

Requires Python 3.11+, bcrypt==5.0.0, OpenSSL, PowerShell 7.4+, nats-server.
All credentials, certificates and JetStream test data are ephemeral and private.
"""
import argparse
import base64
import concurrent.futures
import json
import os
from pathlib import Path
import secrets
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.request

import bcrypt

ROOT = Path(__file__).resolve().parents[1]


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class Wire:
    def __init__(self, port, ca, user, password):
        self.sock = socket.create_connection(("127.0.0.1", port), timeout=3)
        self.sock.settimeout(3)
        line = bytearray()
        while not line.endswith(b"\r\n") and len(line) < 16384:
            b = self.sock.recv(1)
            if not b:
                raise RuntimeError("INFO missing")
            line += b
        info = json.loads(line[5:])
        assert info["tls_required"] and info["auth_required"]
        self.sock = ssl.create_default_context(cafile=str(ca)).wrap_socket(
            self.sock, server_hostname="127.0.0.1"
        )
        self.reader = self.sock.makefile("rb")
        self.send("CONNECT " + json.dumps({"user": user, "pass": password, "verbose": False}))
        self.send("PING")
        assert self.frame() is None

    def send(self, line):
        self.sock.sendall(line.encode() + b"\r\n")

    def frame(self):
        while True:
            line = self.reader.readline(16384)
            if not line:
                raise EOFError("connection closed")
            if line == b"PING\r\n":
                self.send("PONG")
            elif line == b"PONG\r\n":
                return None
            elif line.startswith(b"-ERR"):
                raise RuntimeError("NATS rejected request")
            elif line.startswith(b"MSG "):
                length = int(line.split()[-1])
                assert 0 <= length <= 65536
                data = self.reader.read(length)
                assert self.reader.read(2) == b"\r\n"
                return json.loads(data)

    def request(self, subject, data):
        inbox = "_INBOX.n0." + secrets.token_hex(12)
        self.send(f"SUB {inbox} 1")
        self.send("PING")
        assert self.frame() is None
        raw = json.dumps(data).encode()
        self.send(f"PUB {subject} {inbox} {len(raw)}")
        self.sock.sendall(raw + b"\r\n")
        reply = self.frame()
        self.send("UNSUB 1")
        assert reply is not None and "error" not in reply
        return reply

    def close(self):
        self.reader.close()
        self.sock.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nats-server", required=True)
    parser.add_argument("--pwsh", default="pwsh")
    args = parser.parse_args()
    os.umask(0o077)
    version = subprocess.check_output([args.nats_server, "--version"], text=True).strip()
    with tempfile.TemporaryDirectory(prefix="cosmos-nats-n0-") as temp:
        work = Path(temp)
        port, monitor = free_port(), free_port()
        while monitor == port or monitor in (4222, 8222):
            monitor = free_port()
        assert port not in (4222, 8222)

        def openssl(*options):
            subprocess.run(["openssl", *options], check=True, cwd=work, capture_output=True)

        for name in ("ca", "wrong-ca"):
            openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", name + ".key", "-out", name + ".pem", "-days", "1", "-subj", "/CN=N0-" + name, "-addext", "basicConstraints=critical,CA:TRUE")
        openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", "server.key", "-out", "server.csr", "-subj", "/CN=N0-isolated")
        (work / "extensions").write_text("subjectAltName=IP:127.0.0.1\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\n")
        openssl("x509", "-req", "-in", "server.csr", "-CA", "ca.pem", "-CAkey", "ca.key", "-set_serial", "1", "-out", "server.pem", "-days", "1", "-extfile", "extensions")

        auth = (ROOT / "templates/auth.config.example").read_text()
        passwords = {}
        for user in ("cosmos", "planet", "chat"):
            password = secrets.token_urlsafe(32)
            passwords[user] = password
            (work / (user + ".password")).write_text(password)
            # Fast hashes are test-only; production runbook requires cost >=11.
            hashed = bcrypt.hashpw(password.encode(), bcrypt.gensalt(rounds=4)).decode()
            auth = auth.replace("<BCRYPT_HASH_" + user.upper() + ">", hashed)
        authorization = json.loads(auth[auth.index("{"):])
        admin_password = secrets.token_urlsafe(32)
        authorization["users"].append({
            "user": "n0_test_operator",
            "password": bcrypt.hashpw(admin_password.encode(), bcrypt.gensalt(rounds=4)).decode(),
            "permissions": {
                "publish": {"allow": ["$JS.API.STREAM.CREATE.N0_ISOLATED", "$JS.API.STREAM.INFO.N0_ISOLATED", "$JS.API.STREAM.MSG.GET.N0_ISOLATED", "n0.persistence"]},
                "subscribe": {"allow": ["_INBOX.n0.>"]},
            },
        })
        (work / "auth.config").write_text("authorization " + json.dumps(authorization))
        # Use the proposed production template, adapting only isolated resources.
        config = (ROOT / "templates/server-tls.config.example").read_text()
        for old, new in {
            '"cosmoNATS"': '"N0-isolated-only"',
            '"192.168.111.44:4222"': json.dumps(f"127.0.0.1:{port}"),
            '"127.0.0.1:8222"': json.dumps(f"127.0.0.1:{monitor}"),
            '"D:/cosmosimulator/nats/certs/server-cert.pem"': json.dumps(str(work / "server.pem")),
            '"D:/cosmosimulator/nats/certs/server-key.pem"': json.dumps(str(work / "server.key")),
            '"D:/cosmosimulator/nats/data"': json.dumps(str(work / "jetstream")),
            '"D:/cosmosimulator/nats/logs/server.log"': json.dumps(str(work / "server.log")),
        }.items():
            config = config.replace(old, new)
        path = work / "server.config"
        path.write_text(config)
        subprocess.run([args.nats_server, "-t", "-c", str(path)], check=True, capture_output=True)
        process = None

        def start():
            nonlocal process
            process = subprocess.Popen([args.nats_server, "-c", str(path)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError("isolated broker failed to start; no production process touched")
                try:
                    with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                        return
                except OSError:
                    time.sleep(0.05)
            raise TimeoutError("isolated broker startup")

        def stop():
            if process is not None and process.poll() is None:
                process.terminate()  # Only our own child process.
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=3)

        try:
            start()
            subprocess.run([args.pwsh, "-NoLogo", "-NoProfile", "-File", str(ROOT / "scripts/Test-NatsN0Acl.ps1"), "-SecretDirectory", str(work), "-CAFile", str(work / "ca.pem"), "-WrongCAFile", str(work / "wrong-ca.pem"), "-Port", str(port)], check=True)
            with urllib.request.urlopen(f"http://127.0.0.1:{monitor}/jsz", timeout=3) as response:
                state = json.load(response)
            assert not state.get("disabled", False)
            assert state["config"]["max_memory"] == 512 * 1024**2
            assert state["config"]["max_storage"] == 10 * 1024**3
            print("PASS JetStream enabled; configured limits 512MB/10GB")
            admin = Wire(port, work / "ca.pem", "n0_test_operator", admin_password)
            admin.request("$JS.API.STREAM.CREATE.N0_ISOLATED", {"name": "N0_ISOLATED", "subjects": ["n0.persistence"], "storage": "file", "max_msgs": 10, "max_bytes": 1048576})
            assert admin.request("n0.persistence", {"fixture": "N0 only"})["seq"] == 1
            stop()
            try:
                admin.send("PING")
                admin.frame()
                raise AssertionError("old connection survived broker stop")
            except (OSError, EOFError):
                pass
            finally:
                admin.close()

            def reconnect():
                for _ in range(40):
                    try:
                        return Wire(port, work / "ca.pem", "n0_test_operator", admin_password)
                    except (OSError, EOFError):
                        time.sleep(0.1)
                raise TimeoutError("test client reconnect")

            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
                future = pool.submit(reconnect)
                time.sleep(0.4)
                start()
                admin = future.result(timeout=10)
            try:
                assert admin.request("$JS.API.STREAM.INFO.N0_ISOLATED", {})["state"]["messages"] == 1
                stored = admin.request("$JS.API.STREAM.MSG.GET.N0_ISOLATED", {"seq": 1})["message"]
                assert stored["seq"] == 1
                assert json.loads(base64.b64decode(stored["data"])) == {"fixture": "N0 only"}
            finally:
                admin.close()
            print("PASS reauthenticated reconnect after isolated restart; stored message survives")
            print("PASS test-only lifecycle; production ports and storage never selected")
            print("Server tested:", version)
        finally:
            stop()


if __name__ == "__main__":
    main()
