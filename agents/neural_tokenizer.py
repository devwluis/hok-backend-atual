#!/data/data/com.termux/files/usr/bin/python3
"""
neural_tokenizer.py - Tokenizador Cognitivo (Hokma Neural Codec)
Compacta dados complexos de telemetria em um array de tokens discretos (Codecs)
que a IA pode ler e processar como seu "subconsciente".
"""
import os
import sqlite3
import shutil
from datetime import datetime

DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")

def obter_telemetria():
    uso = shutil.disk_usage("/data/data/com.termux/files/home")
    percent_disk_free = int((uso.free / uso.total) * 100)
    
    percent_battery = 80 # default
    try:
        with open("/sys/class/power_supply/battery/capacity", "r") as f:
            percent_battery = int(f.read().strip())
    except:
        pass
        
    return percent_battery, percent_disk_free

def codificar_tokens(battery, disk_free):
    # Converte dados contínuos em tokens neurais discretos (Codecs)
    # Define o "Humor" (Mood) do sistema: se bateria > 20 e disco > 15 -> Mood: 001 (Saudável), senão 002 (Critico)
    mood = "001"
    if battery < 20 or disk_free < 15:
        mood = "002"
        
    token_string = f"HOK_CODEC://BATT:{battery:03d}_DISK:{disk_free:03d}_MOOD:{mood}"
    return token_string

def salvar_estado_neural(token_string):
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS neural_states (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            token_string TEXT NOT NULL
        )
    """)
    
    # Mantém apenas o estado neural mais recente (limpa os antigos)
    cursor.execute("DELETE FROM neural_states")
    cursor.execute("INSERT INTO neural_states (token_string) VALUES (?)", (token_string,))
    conn.commit()
    conn.close()
    print(f"🧬 [CODEC] Subconsciente tokenizado: {token_string}")

def main():
    batt, disk = obter_telemetria()
    codecs = codificar_tokens(batt, disk)
    salvar_estado_neural(codecs)

if __name__ == "__main__":
    main()
