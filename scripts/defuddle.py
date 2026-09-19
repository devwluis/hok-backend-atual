#!/usr/bin/env python3
import sys
import re
import ipaddress
import socket
import subprocess
from urllib.parse import urlparse
import requests

def validate_url(url):
    if not url.startswith('http://') and not url.startswith('https://'):
        return False, "URL deve começar com http:// ou https://"
    parsed = urlparse(url)
    hostname = parsed.hostname
    if not hostname:
        return False, "URL inválida: sem hostname"
    if re.match(r'^\d+\.\d+\.\d+\.\d+$', hostname):
        try:
            ip = ipaddress.ip_address(hostname)
            if ip.is_private or ip.is_loopback or ip.is_link_local:
                return False, f"URL rejeitada: IP {hostname} é privado/loopback/link-local"
        except ValueError:
            return False, f"IP inválido: {hostname}"
    else:
        try:
            resolved = socket.getaddrinfo(hostname, None)
            for family, type, proto, canonname, sockaddr in resolved:
                ip = ipaddress.ip_address(sockaddr[0])
                if ip.is_private or ip.is_loopback or ip.is_link_local:
                    return False, f"URL rejeitada: domínio {hostname} resolve para IP privado/loopback/link-local ({sockaddr[0]})"
        except (socket.gaierror, ValueError):
            pass
    return True, ""

def main():
    if len(sys.argv) < 2:
        print("Uso: python3 defuddle.py <url>", file=sys.stderr)
        sys.exit(1)
    url = sys.argv[1]
    valid, error = validate_url(url)
    if not valid:
        print(f"Erro: {error}", file=sys.stderr)
        sys.exit(1)
    try:
        response = requests.get(url, timeout=15, allow_redirects=True, headers={"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36"})
        for resp in response.history:
            v, e = validate_url(resp.url)
            if not v:
                print(f"Erro: Redirect rejeitado → {resp.url}: {e}", file=sys.stderr)
                sys.exit(1)
        v, e = validate_url(response.url)
        if not v:
            print(f"Erro: URL final rejeitada → {response.url}: {e}", file=sys.stderr)
            sys.exit(1)
        html = response.text
        if not html.strip():
            print("Erro: resposta vazia do servidor", file=sys.stderr)
            sys.exit(1)
        defuddle_cli = "/root/hokma/backend/scripts/defuddle/node_modules/defuddle/dist/cli.js"
        process = subprocess.run(
            ["node", defuddle_cli, "parse", "-", "--markdown"],
            input=html,
            capture_output=True,
            text=True,
            timeout=30,
        )
        if process.returncode != 0:
            print(f"Erro no defuddle: {process.stderr}", file=sys.stderr)
            sys.exit(1)
        print(process.stdout)
    except requests.Timeout:
        print("Erro: timeout de 15s excedido", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()