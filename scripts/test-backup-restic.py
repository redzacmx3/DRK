#!/usr/bin/env python3
"""Integration regression; requires Restic and a populated inventory fixture."""
import argparse
import json
import os
from pathlib import Path
import secrets
import sqlite3
import subprocess
import sys
import tempfile

p = argparse.ArgumentParser()
p.add_argument('--restic', required=True)
p.add_argument('--db', type=Path, required=True)
a = p.parse_args()
script = Path(__file__).with_name('backup-restic.py').resolve()
with tempfile.TemporaryDirectory(prefix='drk-restic-test-') as tmp:
    base = Path(tmp)
    fixture = base / 'source.db'
    with sqlite3.connect(a.db.resolve().as_uri()+'?mode=ro', uri=True) as src, sqlite3.connect(fixture) as dst:
        src.backup(dst)
    writer = sqlite3.connect(fixture)
    assert writer.execute('PRAGMA journal_mode=WAL').fetchone()[0] == 'wal'
    writer.execute('CREATE TABLE restic_probe(id INTEGER PRIMARY KEY, content BLOB NOT NULL)')
    writer.execute('INSERT INTO restic_probe VALUES(1,?)', (secrets.token_bytes(128),))
    writer.commit()
    assert Path(str(fixture)+'-wal').stat().st_size > 0
    a.db = fixture
    password = base / 'password' 
    password.write_text(secrets.token_urlsafe(32))
    password.chmod(0o600)
    env = dict(os.environ, RESTIC_REPOSITORY=str(base / 'repo'), RESTIC_PASSWORD_FILE=str(password))
    def run(*args, ok=True):
        r = subprocess.run(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        assert (r.returncode == 0) == ok, r.stderr
        return r.stdout
    run(a.restic, 'init')
    run(sys.executable, str(script), '--restic', a.restic, 'backup', '--db', str(a.db.resolve()))
    snapshots = json.loads(run(a.restic, 'snapshots', '--json', '--tag', 'drk-inventory'))
    assert len(snapshots) == 1
    snapshot = snapshots[0]['id']
    target = base / 'restored'
    run(sys.executable, str(script), '--restic', a.restic, 'restore', '--snapshot', snapshot, '--target', str(target))
    with sqlite3.connect(a.db.resolve().as_uri()+'?mode=ro', uri=True) as source, sqlite3.connect(target / 'inventory.db') as restored:
        tables = source.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").fetchall()
        for (table,) in tables:
            quoted = '"' + table.replace('"', '""') + '"'
            assert source.execute('SELECT * FROM '+quoted).fetchall() == restored.execute('SELECT * FROM '+quoted).fetchall(), table
    original = (target / 'inventory.db').read_bytes()
    run(sys.executable, str(script), '--restic', a.restic, 'restore', '--snapshot', snapshot, '--target', str(target), ok=False)
    assert (target / 'inventory.db').read_bytes() == original
    run(sys.executable, str(script), '--restic', a.restic, 'restore', '--snapshot', 'latest', '--target', str(base/'latest'), ok=False)
    missing = base / 'missing.db'
    run(sys.executable, str(script), '--restic', a.restic, 'backup', '--db', str(missing), ok=False)
    assert not missing.exists()
    # An authentic Restic snapshot with an inconsistent application manifest must fail.
    corrupt = base / 'corrupt'
    corrupt.mkdir()
    (corrupt / 'inventory.db').write_bytes(original)
    (corrupt / 'manifest.json').write_text(json.dumps({'format': 1, 'sha256': '0'*64}))
    r = subprocess.run([a.restic, 'backup', '--tag', 'drk-inventory', '--json', 'inventory.db', 'manifest.json'], cwd=corrupt, env=env, capture_output=True, text=True, check=True)
    corrupt_id = next(json.loads(line)['snapshot_id'] for line in r.stdout.splitlines() if json.loads(line).get('message_type') == 'summary')
    run(sys.executable, str(script), '--restic', a.restic, 'restore', '--snapshot', corrupt_id, '--target', str(base/'corrupt-restore'), ok=False)
    assert not (base/'corrupt-restore').exists()
    run(a.restic, 'check', '--read-data')
    password.write_text(secrets.token_urlsafe(32))
    run(sys.executable, str(script), '--restic', a.restic, 'restore', '--snapshot', snapshot, '--target', str(base/'bad-key'), ok=False)
    assert not (base/'bad-key').exists()
    writer.close()
print('Restic integration passed: WAL snapshot, encrypted repository, all tables/blobs restored, no overwrite, explicit snapshot, wrong-key failure, corrupt manifest rejection, missing source rejection, full repository check')
