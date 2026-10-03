#!/usr/bin/env python3
"""Run the installers' plist quarantine cleanup against real extended attributes."""

from pathlib import Path
import os
import re
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[1]
INSTALLER = (ROOT / "macos/scripts/install-embedded-payload.sh").read_text(encoding="utf-8")
SOURCE_INSTALLER = (ROOT / "macos/scripts/install-launchdaemon.sh").read_text(encoding="utf-8")


def fail(message: str) -> None:
    print(f"check-macos-packaging: {message}", file=sys.stderr)
    raise SystemExit(1)


# Exercise the actual installer cleanup against real macOS extended attributes.
# Temporary service definitions never get registered with launchd or need root.
for name, script, runtime_variable in (
    ("embedded", INSTALLER, "runtime_plist_path"),
    ("source", SOURCE_INSTALLER, "plist_path"),
):
    cleanup = re.search(r"^for installed_plist in [^\n]+; do\n.*?^done$", script, re.M | re.S)
    if cleanup is None or "com.apple.quarantine" not in cleanup.group():
        fail(f"{name} installer must handle quarantined service definitions")
    if cleanup.end() > script.index("launchctl bootstrap"):
        fail(f"{name} installer must clean plist quarantine before bootstrap")
    if name == "embedded" and cleanup.start() < script.index("/usr/bin/codesign --verify"):
        fail("embedded installer must verify the App before removing plist quarantine")
    if sys.platform != "darwin":
        continue
    with tempfile.TemporaryDirectory(prefix="steer quarantine test ") as directory:
        root = Path(directory)
        environment = os.environ.copy()
        files = []
        for index, variable in enumerate((runtime_variable, "control_plist_path", "subscription_plist_path")):
            source = root / f"payload-{index}.plist"
            source.write_bytes((ROOT / "macos/launchd/com.steer.steer.plist").read_bytes())
            subprocess.run(["/usr/bin/xattr", "-w", "com.steer.packaging-test", "preserve", str(source)], check=True)
            if index != 1:  # A mixed install also covers files without quarantine.
                subprocess.run(["/usr/bin/xattr", "-w", "com.apple.quarantine", "0081;00000000;SteerTest;", str(source)], check=True)
            destination = root / f"installed-{index}.plist"
            subprocess.run(["/usr/bin/install", "-m", "0644", str(source), str(destination)], check=True)
            environment[variable] = str(destination)
            files.append((source, destination))
        for _ in range(2):  # Repair is idempotent.
            subprocess.run(["/bin/sh", "-eu"], input=cleanup.group(), text=True, env=environment, check=True)
            for index, (source, destination) in enumerate(files):
                attributes = subprocess.check_output(["/usr/bin/xattr", str(destination)], text=True).splitlines()
                if "com.apple.quarantine" in attributes:
                    fail(f"{name} installer left a service definition quarantined")
                if destination.read_bytes() != source.read_bytes():
                    fail(f"{name} installer changed plist contents")
                metadata = subprocess.check_output(["/usr/bin/xattr", "-p", "com.steer.packaging-test", str(destination)], text=True).strip()
                if metadata != "preserve":
                    fail(f"{name} installer removed unrelated metadata")
                source_attributes = subprocess.check_output(["/usr/bin/xattr", str(source)], text=True).splitlines()
                if index != 1 and "com.apple.quarantine" not in source_attributes:
                    fail(f"{name} installer modified the source payload")
        files[0][1].unlink()
        result = subprocess.run(["/bin/sh", "-eu"], input=cleanup.group(), text=True,
                                env=environment, capture_output=True)
        if result.returncode == 0:
            fail(f"{name} installer ignored a plist metadata read failure")

print("macOS installer quarantine checks passed")
