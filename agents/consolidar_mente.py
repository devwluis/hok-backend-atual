#!/data/data/com.termux/files/usr/bin/python3
import sqlite3
import os
import json
import urllib.request
from datetime import datetime

DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")
DS_KEY = "sk-a422b6d95a334f04b82cc2838f3c8e29"
DS_URL = "https://api.deepseek.com/v1/chat/completions"

def obter_historico_conversa():
    if not os.path.exists(DB_PATH):
        return []
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    try:
        cursor.execute("SELECT role, content FROM memory ORDER BY ts DESC LIMIT 30")
        rows = cursor.fetchall()
    except sqlite3.OperationalError:
        rows = []
    conn.close()
    rows.reverse()
    return [{"role": r[0], "content": r[1]} for r in rows]

def solicitar_reflexao_r1(historico):
    if len(historico) == 0:
        return "Nenhuma conversa registrada para consolidação."
    historico_texto = ""
    for h in historico:
        historico_texto += f"[{h['role'].upper()}]: {h['content']}\n"

    prompt_sistema = """Você é o algoritmo de Consolidação Cognitiva do Hokmá v14.
Sua tarefa é analisar o histórico de conversas do Criador (Washington) e extrair:
1. Preferências declaradas pelo usuário de forma explícita ou implícita.
2. Projetos ativos ou tarefas pendentes que ele mencionou.
3. Lições e aprendizados técnicos acordados entre vocês hoje.
Sintetize em formato de tópicos de conhecimento em português."""

    payload = {
        "model": "deepseek-reasoner",
        "messages": [
            {"role": "system", "content": prompt_sistema},
            {"role": "user", "content": "Analise as interações do dia:\n\n" + historico_texto}
        ],
        "max_tokens": 1024
    }
    req_data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(DS_URL, data=req_data)
    req.add_header("Authorization", "Bearer " + DS_KEY)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            res_body = response.read()
            data = json.loads(res_body.decode("utf-8"))
            return data["choices"][0]["message"]["content"]
    except Exception as e:
        return f"Erro de conexão R1: {str(e)}"

def salvar_memoria_longo_prazo(insights):
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS memories (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            key TEXT UNIQUE,
            value TEXT
        )
    """)
    chave_memoria = f"reflexao_cognitiva_{datetime.now().strftime('%Y%m%d')}"
    cursor.execute(
        "INSERT OR REPLACE INTO memories (key, value) VALUES (?, ?)",
        (chave_memoria, insights)
    )
    # Alinhado com o esquema real do Go (event, level, source)
    cursor.execute(
        "INSERT INTO logs (event, level, source) VALUES (?, ?, ?)",
        (f"Consolidação de memória diária concluída. Chave: {chave_memoria}", "SUCCESS", "consolidar_mente")
    )
    conn.commit()
    conn.close()
    print(f"🧠 Mente consolidada sob a chave: {chave_memoria}")

def main():
    historico = obter_historico_conversa()
    if len(historico) == 0:
        print("Nenhum diálogo encontrado hoje para reflexão.")
        return
    insights = solicitar_reflexao_r1(historico)
    print("\n📝 Insights:")
    print(insights)
    salvar_memoria_longo_prazo(insights)

if __name__ == "__main__":
    main()
