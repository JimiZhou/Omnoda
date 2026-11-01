#!/usr/bin/env python3
"""Authorized diagnose command. Prints HEALTHY or UNHEALTHY and exits accordingly."""
import os
import pathlib
import sys
import urllib.request

root = pathlib.Path(__file__).resolve().parent
log = root / "service.log"
snippet = ""
if log.exists():
    lines = log.read_text(errors="replace").splitlines()
    snippet = "\n".join(lines[-20:])

url = os.environ.get("DEMO_HEALTH_URL", "http://127.0.0.1:8765/health")
try:
    with urllib.request.urlopen(url, timeout=2) as resp:
        body = resp.read().decode()
        code = resp.status
except Exception as exc:
    print("UNHEALTHY")
    print(f"error: {exc}")
    if snippet:
        print("--- log ---")
        print(snippet)
    sys.exit(1)

if code == 200 and '"ok"' in body.replace(" ", ""):
    print("HEALTHY")
    print(body)
    sys.exit(0)

print("UNHEALTHY")
print(f"status={code}")
print(body)
if snippet:
    print("--- log ---")
    print(snippet)
sys.exit(1)
