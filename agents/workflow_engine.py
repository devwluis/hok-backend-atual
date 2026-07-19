#!/data/data/com.termux/files/usr/bin/python3
"""
workflow_engine.py - Motor de Automação e Regras do Hokmá v14
Consulta a tabela de workflows e executa ações dinâmicas baseadas em gatilhos.
"""
import sqlite3
import os
import subprocess

DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")

def disparar_evento(evento):
    """Busca o evento no banco de dados e executa a ação programada pelo usuário."""
    if not os.path.exists(DB_PATH):
        return
        
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    
    # Garantir que a tabela existe
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS workflows (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            trigger_event TEXT UNIQUE NOT NULL,
            action_command TEXT NOT NULL
        )
    """)
    
    cursor.execute("SELECT action_command FROM workflows WHERE trigger_event = ?", (evento,))
    row = cursor.fetchone()
    conn.close()
    
    if row:
        comando = row[0]
        print(f"⚡ [AUTOMACAO] Evento '{evento}' disparado! Executando: {comando}")
        # Executa a automação de forma segura
        subprocess.run(comando, shell=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    else:
        print(f"ℹ️ [AUTOMACAO] Nenhum workflow dinâmico registrado para o evento '{evento}'.")

if __name__ == "__main__":
    import sys
    if len(sys.argv) > 1:
        disparar_evento(sys.argv[1])
    else:
        print("Uso: python3 workflow_engine.py <nome_do_evento>")
