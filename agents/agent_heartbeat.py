#!/data/data/com.termux/files/usr/bin/python3
"""
agent_heartbeat.py - Sincronizador de Estado Neural com suporte a Automação
Módulo de Saúde e Cognição - Caminho E e F (Hokmá v14)
"""
import os
import sqlite3
import shutil
import subprocess
from datetime import datetime

DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")

def obter_dados_reais():
    uso = shutil.disk_usage("/data/data/com.termux/files/home")
    percent_disk_free = int((uso.free / uso.total) * 100)
    
    percent_battery = 80
    try:
        with open("/sys/class/power_supply/battery/capacity", "r") as f:
            percent_battery = int(f.read().strip())
    except:
        try:
            bat_json = subprocess.check_output(["termux-battery-status"], timeout=3).decode("utf-8")
            import json
            bat_map = json.loads(bat_json)
            percent_battery = int(bat_map.get("percentage", 80))
        except:
            pass
            
    return percent_battery, percent_disk_free

def sincronizar():
    batt, disk_free = obter_dados_reais()
    
    mood = "001" 
    if batt < 20 or disk_free < 15:
        mood = "002"
        
    token_string = f"HOK_CODEC://BATT:{batt:03d}_DISK:{100-disk_free:03d}_MOOD:{mood}"
    
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute("INSERT OR REPLACE INTO memories (key, value) VALUES ('last_known_battery', ?)", (str(batt),))
    
    # Gravar o token do subconsciente
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS neural_states (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            token_string TEXT NOT NULL
        )
    """)
    cursor.execute("DELETE FROM neural_states")
    cursor.execute("INSERT INTO neural_states (token_string) VALUES (?)", (token_string,))
    
    cursor.execute("INSERT INTO logs (event, level, source) VALUES (?, ?, ?)", 
        (f"Sincronização concluída: {token_string}", "SUCCESS", "agent_heartbeat"))
    
    conn.commit()
    conn.close()
    
    # DISPARAR GATILHO DE AUTOMAÇÃO NO BANCO DE DADOS
    if batt < 20:
        subprocess.run(["python3", "/data/data/com.termux/files/home/ecossistema/backend/agents/workflow_engine.py", "bateria_critica"])
        
    print(f"🧬 [HEARTBEAT] Sincronizado: {token_string}")

if __name__ == "__main__":
    sincronizar()
