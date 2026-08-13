#!/usr/bin/env python3
"""
Conecta o agente hok-saude ao Telegram via n8n 2.34.
Atualiza o workflow 'HOK OS Multi IA Telegram' para usar o node
'AI Agent Tool (AgentTool)' em vez do 'Call HOK Backend' HTTP.

PASSOS:
1. PATCH /rest/mcp/settings — habilita MCP nativo (agente exposto via MCP)
2. Atualiza o agente hok-saude: availableInMCP=true + adiciona tools (ToolWorkflow, MCP tools)
3. Atualiza o workflow Telegram: substitui 'Call HOK Backend' HTTP por 'AI Agent Tool'
4. Reativa o workflow
"""

import json
import os
import requests
import sys

# ── Configuração ─────────────────────────────────────────────────────────
N8N_BASE = "http://127.0.0.1:5678"
N8N_API_KEY = os.environ.get("N8N_API_KEY", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJmNGUzNDdmNy0xNmVhLTRkMzQtYmZmNS0zOWIyM2U4YTBjN2EiLCJpc3MiOiJuOG4iLCJhdWQiOiJwdWJsaWMtYXBpIiwianRpIjoiMmRkYjU2Y2YtZjI0NC00Njk1LTljMTgtODBlNGQ4NjAyYjQ1IiwiaWF0IjoxNzgzNzk2Nzc0fQ.bqrYS0qG6OTlqbZDNnUpdDW8TbdWB91ZJEesdYz2jhE")
AGENT_WORKFLOW_ID = "JBRgJSRbQUX8GlLc"  # hok-saude agent
TELEGRAM_WORKFLOW_ID = "SFd42XABa4HvpPXT"  # HOK OS Multi IA Telegram
TELEGRAM_BOT_CRED_ID = "RsxUnPy7wMDSO5hK"  # Hokma Telegram Bot
OPENROUTER_CRED_ID = "T0Xd5mw8fldmAHR5"  # OpenRouter (hok-saude)
BACKEND_AUTH_CRED_ID = "pRSYxrXdJIe8aZlE"  # HOK Backend Auth

HEADERS = {
    "X-N8N-API-KEY": N8N_API_KEY,
    "Content-Type": "application/json",
}

# ── Passo 1: Habilitar MCP nativo no n8n ────────────────────────────────
# PATCH /rest/mcp/settings — expõe agentes via MCP
# Esta API é específica do n8n 2.34 para agents preview
def enable_mcp_native():
    print("[1/4] Habilitando MCP nativo no n8n...")
    payload = {
        "mcp": {
            "access": {
                "enabled": True
            }
        }
    }
    resp = requests.post(
        f"{N8N_BASE}/api/v1/mcp/settings",
        headers=HEADERS,
        json=payload,
        timeout=10
    )
    if resp.status_code in (200, 201, 204):
        print("  ✓ MCP nativo habilitado")
        return True
    else:
        print(f"  ⚠ MCP settings: {resp.status_code} — {resp.text}")
        # Tenta PATCH alternativo
        resp2 = requests.patch(f"{N8N_BASE}/rest/mcp/settings", headers=HEADERS, json=payload, timeout=10)
        print(f"  • PATCH /rest/mcp/settings: {resp2.status_code}")
        return resp2.status_code in (200, 201, 204)

# ── Passo 2: Atualizar agente hok-saude ───────────────────────────────────
# Set availableInMCP=true e adiciona tools reais
def update_agent():
    print("[2/4] Atualizando agente hok-saude (availableInMCP + tools)...")
    wf_data = get_workflow(AGENT_WORKFLOW_ID)
    if not wf_data:
        print("  ✗ Não foi possível obter o agente hok-saude")
        return False

    # Atualiza os parâmetros do agente
    nodes = wf_data.get("nodes", [])
    for node in nodes:
        if node.get("type", "").startswith("@n8n/n8n-nodes-langchain"):
            params = node.get("parameters", {})
            # Enable MCP
            params["availableInMCP"] = True
            # Adiciona tools reais
            if "tools" not in params:
                params["tools"] = []
            if "httpRequest" not in params["tools"]:
                params["tools"].append("httpRequest")
            if "code" not in params["tools"]:
                params["tools"].append("code")
            # ToolWorkflow para chamar workflows HOK existentes
            if "mcp" not in params["tools"]:
                params["tools"].append("mcp")
            node["parameters"] = params
            print(f"  ✓ Node {node['name']} atualizado: availableInMCP=True, tools={params['tools']}")

    # Atualiza o workflow no n8n
    resp = requests.patch(
        f"{N8N_BASE}/api/v1/workflows/{AGENT_WORKFLOW_ID}",
        headers=HEADERS,
        json=w_data_to_update(wf_data),
        timeout=10
    )
    if resp.status_code == 200:
        print("  ✓ Agente hok-saude atualizado")
        return True
    else:
        print(f"  ✗ Erro atualizando agente: {resp.status_code} {resp.text[:300]}")
        return False

def get_workflow(wf_id):
    resp = requests.get(f"{N8N_BASE}/api/v1/workflows/{wf_id}", headers=HEADERS, timeout=10)
    if resp.status_code == 200:
        return resp.json()
    return None

def wf_data_to_update(wf_data):
    return {
        "nodes": wf_data.get("nodes"),
        "connections": wf_data.get("connections"),
        "name": wf_data.get("name"),
        "active": wf_data.get("active", True),
        "settings": wf_data.get("settings", {}),
    }

# ── Passo 3: Atualizar workflow do Telegram ───────────────────────────────
# Substitui 'Call HOK Backend' HTTP por 'AI Agent Tool' que chama hok-saude
def update_telegram_workflow():
    print("[3/4] Atualizando workflow HOK OS Multi IA Telegram...")
    wf_data = get_workflow(TELEGRAM_WORKFLOW_ID)
    if not wf_data:
        print("  ✗ Não foi possível obter o workflow Telegram")
        return False

    nodes = wf_data.get("nodes", [])
    connections = wf_data.get("connections", {})

    # --- Substitui o node 'Call HOK Backend' pelo 'AI Agent Tool' ---
    new_nodes = []
    for node in nodes:
        if node.get("name") == "Call HOK Backend":
            # Substitui pelo AI Agent Tool (Message an Agent)
            print(f"  → Substituindo '{node['name']}' por 'HOK Agente Saude'")
            # Cria o novo node AgentTool apontando para hok-saude
            agent_node = {
                "parameters": {
                    "agentId": AGENT_WORKFLOW_ID,  # hok-saude agent
                    "message": "={{ $('Parse').item.json.text }}",
                    "options": {
                        "timeout": 60,
                        "maxIterations": 5
                    }
                },
                "id": "agent_telegram_" + AGENT_WORKFLOW_ID[:8],
                "name": "HOK Agente Saude",
                "type": "@n8n/n8n-nodes-langchain.agentTool",
                "typeVersion": 3,
                "position": [1120, 224]  # mesma posicao do antigo
            }
            new_nodes.append(agent_node)
        else:
            new_nodes.append(node)

    # --- Atualiza as conexões: 'Typing' → 'HOK Agente Saude' → 'Send Reply' ---
    # O antigo 'Call HOK Backend' recebia de 'Typing' e enviava para 'Send Reply'
    new_connections = {}
    for key, val in connections.items():
        if key == "Typing":
            # Redireciona Typing → HOK Agente Saude (em vez de → Call HOK Backend)
            new_conns = []
            for v in val:
                for item in v:
                    if item.get("node") == "Call HOK Backend":
                        item["node"] = "HOK Agente Saude"
                        # Ajusta a porta de saída para agentMessage
                        item["index"] = 0  # main output
                    # Ajusta para usar a porta de saída correta do AgentTool
            new_conns = val
            new_connections[key] = new_conns
        elif key == "Call HOK Backend":
            # O output do Call HOK Backend (response) agora vem de HOK Agente Saude
            new_connections["HOK Agente Saude"] = val
        elif key == "Parse":
            # Parse também alimenta o agente
            new_connections[key] = val
        else:
            new_connections[key] = val

    # Remove referências ao antigo node
    if "Call HOK Backend" in new_connections:
        del new_connections["Call HOK Backend"]

    # Atualiza os dados do workflow
    wf_data["nodes"] = new_nodes
    wf_data["connections"] = new_connections

    # Salva no n8n
    resp = requests.patch(
        f"{N8N_BASE}/api/v1/workflows/{TELEGRAM_WORKFLOW_ID}",
        headers=HEADERS,
        json={
            "nodes": new_nodes,
            "connections": new_connections,
            "name": wf_data.get("name"),
            "active": True,
            "settings": wf_data.get("settings", {}),
        },
        timeout=10
    )
    if resp.status_code == 200:
        print("  ✓ Workflow Telegram atualizado")
        return True
    else:
        print(f"  ✗ Erro: {resp.status_code} {resp.text[:300]}")
        return False

# ── Passo 4: Reativar workflows ───────────────────────────────────────────
def activate_workflow(wf_id):
    resp = requests.patch(
        f"{N8N_BASE}/api/v1/workflows/{wf_id}/activate",
        headers=HEADERS,
        timeout=10
    )
    return resp.status_code in (200, 204)

# ── Main ──────────────────────────────────────────────────────────────────
def main():
    print("=" * 70)
    print("CONEXAO AGENTE hok-saude ↔ TELEGRAM (n8n 2.34.4)")
    print("=" * 70)

    results = {}
    results["mcp_native"] = enable_mcp_native()
    results["agent_updated"] = update_agent()
    results["workflow_updated"] = update_telegram_workflow()

    print("\n[4/4] Reativando workflows...")
    if activate_workflow(AGENT_WORKFLOW_ID):
        print("  ✓ hok-saude agent reativado")
    if activate_workflow(TELEGRAM_WORKFLOW_ID):
        print("  ✓ HOK OS Multi IA Telegram reativado")

    print("\n" + "=" * 70)
    print("RESUMO:")
    for k, v in results.items():
        status = "✓ CONCLUIDO" if v else "✗ FALHOU"
        print(f"  {k}: {status}")
    print("=" * 70)
    return 0 if all(results.values()) else 1

if __name__ == "__main__":
    sys.exit(main())
