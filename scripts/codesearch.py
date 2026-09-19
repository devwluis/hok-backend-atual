#!/usr/bin/env python3
"""codebase-memory — SQLite FTS5-based codebase search for HOK OS."""
import sqlite3
import os
import sys
import argparse
import datetime
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
INDEX_DIR = REPO_ROOT / ".codesearch"
INDEX_DB = INDEX_DIR / "index.db"
CONFIG_FILE = INDEX_DIR / "config"
SKIP_DIRS = {".git", "node_modules", "__pycache__", ".codesearch", "vendor", ".tmp", "tmp", "venv"}
SKIP_DIR_PATTERNS = ("backup_", ".backup", "_bak", "backup")
SKIP_FILE_SUBSTRINGS = (".bak", ".backup", ".tmp", ".swp", ".orig", ".docx")
SKIP_FILE_PATTERNS = ("backup", "Backup", "BACKUP")
INDEX_EXTENSIONS = {".go", ".ts", ".js", ".md", ".yaml", ".yml", ".json", ".toml", ".sh"}
MAX_LINE_LENGTH = 500
MAX_FILES = 5000


def needs_rebuild():
    if not INDEX_DB.exists():
        return True
    index_mtime = INDEX_DB.stat().st_mtime
    for root, dirs, files in os.walk(REPO_ROOT):
        root_path = Path(root)
        if any(d in SKIP_DIRS for d in root_path.parts):
            continue
        if any(any(pat in part for pat in SKIP_DIR_PATTERNS) for part in root_path.parts):
            continue
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS and not any(pat in d for pat in SKIP_DIR_PATTERNS)]
        for fname in files:
            fpath = root_path / fname
            ext = fpath.suffix.lower()
            if ext not in INDEX_EXTENSIONS:
                continue
            if fpath.name.startswith("."):
                continue
            if fpath.stat().st_mtime > index_mtime:
                return True
    return False


def init_index(force=False):
    if INDEX_DIR.exists() and INDEX_DB.exists() and not force and not needs_rebuild():
        existing = read_config()
        print(f"Index already exists: {INDEX_DB}")
        print(f"  Files: {existing.get('files_count', '?')}, Lines: {existing.get('lines_count', '?')}")
        print(f"  Indexed at: {existing.get('indexed_at', '?')}")
        print("Use --force to rebuild.")
        return
    if not INDEX_DIR.exists():
        INDEX_DIR.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(INDEX_DB))
    if force:
        conn.execute("DROP TABLE IF EXISTS code_index")
        conn.execute("DROP TABLE IF EXISTS meta")
    conn.execute("""CREATE VIRTUAL TABLE IF NOT EXISTS code_index
        USING fts5(file_path, line_number, content, language, tokenize="unicode61")""")
    files_count = 0
    lines_count = 0
    skipped = 0
    start = datetime.datetime.now()
    for root, dirs, files in os.walk(REPO_ROOT):
        root_path = Path(root)
        if any(d in SKIP_DIRS for d in root_path.parts):
            continue
        if any(any(pat in part for pat in SKIP_DIR_PATTERNS) for part in root_path.parts):
            continue
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS and not any(pat in d for pat in SKIP_DIR_PATTERNS)]
        for fname in sorted(files):
            fpath = root_path / fname
            rel = str(fpath.relative_to(REPO_ROOT))
            if any(s in fname for s in SKIP_FILE_SUBSTRINGS):
                skipped += 1
                continue
            if any(s in fname for s in SKIP_FILE_PATTERNS):
                skipped += 1
                continue
            ext = fpath.suffix.lower()
            if ext not in INDEX_EXTENSIONS:
                skipped += 1
                continue
            if fpath.name.startswith("."):
                skipped += 1
                continue
            try:
                with open(fpath, "r", encoding="utf-8", errors="replace") as f:
                    lines = f.readlines()
            except Exception:
                skipped += 1
                continue
            if len(lines) > MAX_LINE_LENGTH * 100:
                skipped += 1
                continue
            batch = []
            for i, line in enumerate(lines, 1):
                line_text = line.rstrip("\n\r")[:MAX_LINE_LENGTH]
                if not line_text.strip():
                    continue
                batch.append((rel, i, line_text, ext.lstrip(".")))
            if batch:
                conn.executemany(
                    "INSERT INTO code_index (file_path, line_number, content, language) VALUES (?,?,?,?)",
                    batch,
                )
                conn.commit()
                files_count += 1
                lines_count += len(batch)
            if files_count >= MAX_FILES:
                break
        if files_count >= MAX_FILES:
            break
    elapsed = (datetime.datetime.now() - start).total_seconds()
    conn.execute("""CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)""")
    now = datetime.datetime.now().isoformat()
    conn.execute("INSERT OR REPLACE INTO meta VALUES (?,?)", ("files_count", str(files_count)))
    conn.execute("INSERT OR REPLACE INTO meta VALUES (?,?)", ("lines_count", str(lines_count)))
    conn.execute("INSERT OR REPLACE INTO meta VALUES (?,?)", ("indexed_at", now))
    conn.commit()
    conn.execute("VACUUM")
    conn.close()
    write_config(files_count, lines_count, now)
    print(f"Indexed {files_count} files, {lines_count} lines in {elapsed:.1f}s")
    print(f"Index: {INDEX_DB}")


def read_config():
    if not CONFIG_FILE.exists():
        return {}
    cfg = {}
    for line in CONFIG_FILE.read_text().splitlines():
        if "=" in line:
            k, v = line.split("=", 1)
            cfg[k.strip()] = v.strip()
    return cfg


def write_config(files_count, lines_count, indexed_at):
    CONFIG_FILE.write_text(
        f"repo_root={REPO_ROOT}\n"
        f"files_count={files_count}\n"
        f"lines_count={lines_count}\n"
        f"indexed_at={indexed_at}\n"
    )


def query_index(query_text, limit=20):
    if not INDEX_DB.exists():
        print("Error: index not found. Run: codebase-memory --init")
        sys.exit(1)
    conn = sqlite3.connect(str(INDEX_DB))
    conn.row_factory = sqlite3.Row
    rows = conn.execute(
        """SELECT file_path, line_number, content, language,
                  bm25(code_index) as rank
           FROM code_index
           WHERE code_index MATCH ?
           ORDER BY rank
           LIMIT ?""",
        (query_text, limit),
    ).fetchall()
    conn.close()
    if not rows:
        print(f"No results for: {query_text}")
        return
    seen = set()
    for r in rows:
        fp = r["file_path"]
        if fp in seen:
            continue
        seen.add(fp)
        print(f"{fp}:{r['line_number']} [{r['language']}]")
        print(f"  {r['content']}")
        print()


def get_status():
    if not INDEX_DB.exists():
        print("No index found. Run: codebase-memory --init")
        return
    cfg = read_config()
    print(f"Index: {INDEX_DB}")
    print(f"Files: {cfg.get('files_count', '?')}")
    print(f"Lines: {cfg.get('lines_count', '?')}")
    print(f"Indexed at: {cfg.get('indexed_at', '?')}")


def main():
    parser = argparse.ArgumentParser(description="codebase-memory")
    parser.add_argument("query", nargs="?", help="Search query (or use --init/--status)")
    parser.add_argument("--init", action="store_true", help="Index the repo")
    parser.add_argument("--force", action="store_true", help="Rebuild index")
    parser.add_argument("--status", action="store_true", help="Show index status")
    parser.add_argument("--limit", type=int, default=20, help="Max results")
    args = parser.parse_args()

    if args.init:
        init_index(force=args.force)
        return
    if args.status:
        get_status()
        return
    if not args.query:
        parser.print_help()
        sys.exit(1)
    query_index(args.query, args.limit)


if __name__ == "__main__":
    main()
