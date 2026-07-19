#!/data/data/com.termux/files/usr/bin/python3
"""
agent_location.py - Percepção de Espaço com suporte a Automação
Lê o satélite GPS, calcula as distâncias e aciona o motor de automações do SQLite.
"""
import os
import sqlite3
import subprocess
import json
import math
from datetime import datetime

DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")
LIMIAR_DISTANCIA = 100.0

def calcular_distancia(lat1, lon1, lat2, lon2):
    R = 6371000.0
    phi1 = math.radians(lat1)
    phi2 = math.radians(lat2)
    dphi = math.radians(lat2 - lat1)
    dlambda = math.radians(lon2 - lon1)
    
    a = math.sin(dphi/2.0)**2 + math.cos(phi1)*math.cos(phi2)*math.sin(dlambda/2.0)**2
    c = 2.0 * math.atan2(math.sqrt(a), math.sqrt(1.0 - a))
    return R * c

def obter_gps_atual():
    try:
        out = subprocess.check_output(["termux-location"], timeout=15).decode("utf-8")
        data = json.loads(out)
        return float(data["latitude"]), float(data["longitude"])
    except:
        return -16.686891, -49.264789

def carregar_locais_salvos():
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute("SELECT key, value FROM memories WHERE key LIKE 'geo_%'")
    rows = cursor.fetchall()
    conn.close()
    
    locais = {}
    for r in rows:
        parts = r[1].split(",")
        if len(parts) >= 3:
            locais[r[0]] = {
                "lat": float(parts[0]),
                "lon": float(parts[1]),
                "nome": parts[2]
            }
    return locais

def obter_ultimo_local():
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute("SELECT value FROM memories WHERE key='last_known_location'")
    row = cursor.fetchone()
    conn.close()
    if row:
        return row[0]
    return "Desconhecido"

def salvar_local_atual(nome_local):
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute("INSERT OR REPLACE INTO memories (key, value) VALUES ('last_known_location', ?)", (nome_local,))
    conn.commit()
    conn.close()

def registrar_log(mensagem, nivel="INFO"):
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute("INSERT INTO logs (event, level, source) VALUES (?, ?, ?)", (mensagem, nivel, "agent_location"))
    conn.commit()
    conn.close()

def main():
    print("==================================================")
    print(" 🔍 agent_location.py — Varredura GPS Ativa...")
    print("==================================================")
    
    lat, lon = obter_gps_atual()
    locais = carregar_locais_salvos()

    local_detectado = "Desconhecido"
    for chave, loc in locais.items():
        dist = calcular_distancia(lat, lon, loc["lat"], loc["lon"])
        if dist <= LIMIAR_DISTANCIA:
            local_detectado = loc["nome"]
            break

    ultimo_local = obter_ultimo_local()

    if local_detectado != ultimo_local:
        print(f"\n🚨 [TRANSIÇÃO] Washington moveu-se de {ultimo_local} para {local_detectado}!")
        salvar_local_atual(local_detectado)
        
        # Alertas básicos
        mensagem_log = ""
        if local_detectado == "Desconhecido":
            mensagem_log = f"Washington saiu de {ultimo_local} e está em trânsito."
            subprocess.run(["termux-notification", "-t", "📍 Hokma GPS", "-c", mensagem_log], timeout=5)
        else:
            mensagem_log = f"Washington chegou em {local_detectado}."
            subprocess.run(["termux-notification", "-t", "📍 Bem-vindo!", "-c", f"Hokma ativo em {local_detectado}."], timeout=5)
            subprocess.run(["termux-tts-speak", f"Criador Washington, detectei que você chegou ao seu local de {local_detectado}."], timeout=5)
            
        registrar_log(mensagem_log, "SUCCESS")
        
        # DISPARAR GATILHOS DE AUTOMAÇÃO DINÂMICOS NO BANCO DE DADOS
        if local_detectado != "Desconhecido":
            # Ex: chegada_Casa ou chegada_Trabalho
            evento_gatilho = f"chegada_{local_detectado}"
            subprocess.run(["python3", "/data/data/com.termux/files/home/ecossistema/backend/agents/workflow_engine.py", evento_gatilho])
            
    else:
        print("\nStable state: Localização inalterada.")

    print("==================================================")

if __name__ == "__main__":
    main()
