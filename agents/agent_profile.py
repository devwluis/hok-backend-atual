#!/data/data/com.termux/files/usr/bin/python3
import sqlite3
import os

PERFIL = {
    "nome": "Washington",
    "papel": "Arquiteto e Criador",
    "projeto": "Hokmá v14",
    "data_inicializacao_nucleo": "Junho de 2026"
}
DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")

def registrar_perfil():
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    detalhes = f"Nome: {PERFIL['nome']} | Papel: {PERFIL['papel']} | Projeto: {PERFIL['projeto']} | Inicialização: {PERFIL['data_inicializacao_nucleo']}"
    # Alinhado com o esquema real do Go (event, level, source)
    cursor.execute(
        "INSERT INTO logs (event, level, source) VALUES (?, ?, ?)",
        (detalhes, "INFO", "agent_profile")
    )
    conn.commit()
    conn.close()

def main():
    registrar_perfil()
    print(f"Perfil de {PERFIL['nome']} registrado com sucesso no banco de dados.")

if __name__ == "__main__":
    main()
