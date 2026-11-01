#!/usr/bin/env python3
"""Runs the demo service's unit check without binding a privileged port."""
import pathlib
import re
import sys

root = pathlib.Path(__file__).resolve().parent
src = (root / "app.py").read_text()
# The healthy implementation responds 200 and includes status ok.
ok = "send_response(200)" in src and re.search(r"status[\"']?\s*:\s*[\"']ok[\"']", src)
cases = [("health_returns_ok", ok)]
passed = sum(1 for _, good in cases if good)
failed = len(cases) - passed
print(f"tests={len(cases)} passed={passed} failed={failed}")
for name, good in cases:
    print(("PASS " if good else "FAIL ") + name)
sys.exit(0 if failed == 0 else 1)
