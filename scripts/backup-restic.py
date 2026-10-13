#!/usr/bin/env python3
"""Consistent SQLite snapshots and isolated, verified restoration with Restic."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import tempfile

TAG = "drk-inventory"


def verify(path):
    with sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True) as db:
        if db.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
            raise ValueError("SQLite integrity check failed")
        if db.execute("PRAGMA foreign_key_check").fetchone() is not None:
            raise ValueError("SQLite foreign key check failed")
        # Reject an unrelated SQLite file, even if structurally valid.
        for table in ("schema_migrations", "device_documents", "stock_movements"):
            if not db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (table,)).fetchone():
                raise ValueError("Not an inventory database: missing " + table)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--restic", default="restic")
    commands = parser.add_subparsers(dest="action", required=True)
    backup = commands.add_parser("backup")
    backup.add_argument("--db", type=Path, required=True)
    restore = commands.add_parser("restore")
    restore.add_argument("--snapshot", required=True, help="Explicit snapshot ID; latest is deliberately unsupported")
    restore.add_argument("--target", type=Path, required=True, help="New directory, never the active data directory")
    args = parser.parse_args()
    os.umask(0o077)
    if not os.environ.get("RESTIC_REPOSITORY") or not os.environ.get("RESTIC_PASSWORD_FILE"):
        parser.error("RESTIC_REPOSITORY and RESTIC_PASSWORD_FILE are required")
    if args.action == "restore" and (args.snapshot == "latest" or not args.snapshot or any(c not in "0123456789abcdef" for c in args.snapshot) or len(args.snapshot) < 8 or len(args.snapshot) > 64):
        parser.error("Use an explicit hexadecimal snapshot ID (8–64 characters)")
    with tempfile.TemporaryDirectory(prefix="drk-backup-") as temporary:
        stage = Path(temporary)
        database = stage / "inventory.db"
        if args.action == "backup":
            source = args.db.resolve()
            with sqlite3.connect(source.as_uri() + "?mode=ro", uri=True) as src, sqlite3.connect(database) as dst:
                src.backup(dst)
            verify(database)
            (stage / "manifest.json").write_text(json.dumps({"format": 1, "sha256": digest(database)}, indent=2) + "\n")
            subprocess.run([args.restic, "backup", "--tag", TAG, "--json", "inventory.db", "manifest.json"], cwd=stage, check=True)
        else:
            if args.target.exists():
                raise ValueError("Restore target already exists")
            subprocess.run([args.restic, "restore", args.snapshot, "--tag", TAG, "--target", str(stage)], check=True)
            manifest = json.loads((stage / "manifest.json").read_text())
            if manifest.get("format") != 1 or digest(database) != manifest.get("sha256"):
                raise ValueError("Backup manifest or checksum mismatch")
            verify(database)
            args.target.mkdir(mode=0o700, parents=False, exist_ok=False)
            # Exclusive creation also prevents overwriting a file if another actor races us.
            for name in ("inventory.db", "manifest.json"):
                with (stage / name).open("rb") as src, (args.target / name).open("xb") as dst:
                    shutil.copyfileobj(src, dst)
            print("Verified restoration completed in a separate directory")


if __name__ == "__main__":
    main()
