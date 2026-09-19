#!/usr/bin/env python3
"""hok_state_upload.py — Uploada HOK_STATE.md para Google Drive (UPDATE) se mudou.

Usa google-api-python-client + credenciais de drive_creds.env.
Compara hash SHA256 do arquivo local com o modificationTime no Drive.
Se diferente, faz upload como UPDATE (mantém ID e pastas).
"""
import hashlib
import json
import os
import re
import sys
from datetime import datetime

from google.oauth2.credentials import Credentials
from google.auth.transport.requests import Request
from googleapiclient.discovery import build
from googleapiclient.http import MediaFileUpload

ROOT_DIR = os.environ.get("HOK_ROOT_DIR", "/root/hokma")
STATE_FILE = os.path.join(ROOT_DIR, "HOK_STATE.md")
CREDS_ENV = os.path.join(ROOT_DIR, "backend", "drive_creds.env")
DRIVE_FILE_ID = "1aIqLeytpJBGWwbpeSp_KBkkYXslgIkvb"
HASH_STORE = os.path.join(ROOT_DIR, ".hok_state_hash")

SCOPES = ["https://www.googleapis.com/auth/drive"]


def load_env(path):
    env_vars = {}
    with open(path) as f:
        for line in f:
            line = line.strip()
            if "=" in line and not line.startswith("#"):
                k, v = line.split("=", 1)
                env_vars[k.strip()] = v.strip()
    return env_vars


def get_credentials():
    env = load_env(CREDS_ENV)
    client_id = env.get("DRIVE_CLIENT_ID")
    client_secret = env.get("DRIVE_CLIENT_SECRET")
    refresh_token = env.get("DRIVE_REFRESH_TOKEN")

    if not (client_id and client_secret and refresh_token):
        raise RuntimeError(f"Missing Drive credentials in {CREDS_ENV}")

    creds_data = {
        "client_id": client_id,
        "client_secret": client_secret,
        "refresh_token": refresh_token,
        "token": {"access_token": "", "refresh_token": refresh_token, "scope": SCOPES[0]},
    }
    creds = Credentials.from_authorized_user_info(creds_data, SCOPES)
    if not creds or not creds.valid:
        if creds and creds.expired and creds.refresh_token:
            creds.refresh(Request())
        else:
            raise RuntimeError("Invalid Drive credentials")
    return creds


def local_hash(path):
    h = hashlib.sha256()
    TIMESTAMP_RE = re.compile(
        r"\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}"
    )
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        for i, line in enumerate(f):
            if i == 0:  # skip 1st line (timestamp in header)
                continue
            if TIMESTAMP_RE.search(line):
                continue
            h.update(line.encode("utf-8"))
    return h.hexdigest()


def drive_modified_time(service, file_id):
    meta = service.files().get(fileId=file_id, fields="modifiedTime").execute()
    return meta.get("modifiedTime")


def main():
    if not os.path.exists(STATE_FILE):
        print(f"HOK_STATE.md não encontrado em {STATE_FILE}")
        sys.exit(0)

    current_hash = local_hash(STATE_FILE)

    previous_hash = None
    if os.path.exists(HASH_STORE):
        previous_hash = open(HASH_STORE).read().strip()

    if current_hash == previous_hash:
        print(f"HOK_STATE.md sem mudanças (hash: {current_hash[:12]}...)")
        sys.exit(0)

    try:
        creds = get_credentials()
    except RuntimeError as e:
        print(f"Credenciais Drive indisponíveis: {e}")
        sys.exit(0)

    service = build("drive", "v3", credentials=creds)

    remote_time = drive_modified_time(service, DRIVE_FILE_ID)
    local_mtime = datetime.utcfromtimestamp(os.path.getmtime(STATE_FILE)).isoformat() + "Z"

    if remote_time == local_mtime:
        previous_hash = current_hash
        open(HASH_STORE, "w").write(previous_hash)
        print(f"HOK_STATE.md já sincronizado (remote time: {remote_time})")
        sys.exit(0)

    media = MediaFileUpload(STATE_FILE, mimetype="text/markdown")
    updated = (
        service.files()
        .update(
            fileId=DRIVE_FILE_ID,
            body={"name": "HOK_STATE.md"},
            media_body=media,
            fields="id,modifiedTime",
        )
        .execute()
    )

    open(HASH_STORE, "w").write(current_hash)
    print(f"HOK_STATE.md uploadado (hash: {current_hash[:12]}, time: {updated.get('modifiedTime')})")


if __name__ == "__main__":
    main()
