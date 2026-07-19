#!/data/data/com.termux/files/usr/bin/python3
import os
import sqlite3
import shutil
import subprocess

PARTITION = "/data/data/com.termux/files/home"
THRESHOLD_PERCENT = 15
DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")

def verificar_espaco(particao):
    uso = shutil.disk_usage(particao)
    total_gb = uso.total / (1024**3)
    livre_gb = uso.free / (1024**3)
    percentual_livre = (uso.free / uso.total) * 100
    percentual_usado = 100 - percentual_livre
    return round(percentual_usado, 2), round(percentual_livre, 2), round(total_gb, 2), round(livre_gb, 2)

def alerta_termux(mensagem, tipo="toast"):
    try:
        if tipo == "toast":
            subprocess.run(["termux-toast", "-b", "red", mensagem], timeout=5)
        else:
            subprocess.run(["termux-notification", "-t", "🚨 Alerta de Disco", "-c", mensagem], timeout=5)
    except:
        pass

def registrar_log(status, detalhes):
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    # Alinhado com o esquema real do Go (event, level, source)
    cursor.execute(
        "INSERT INTO logs (event, level, source) VALUES (?, ?, ?)",
        (f"Métricas de disco salvas. {detalhes}", status, "agent_disk")
    )
    conn.commit()
    conn.close()

def main():
    usado, livre, total_gb, livre_gb = verificar_espaco(PARTITION)
    detalhes = f"Usado: {usado}%, Livre: {livre}%, Total: {total_gb}GB, Livre: {livre_gb}GB"
    if livre < THRESHOLD_PERCENT:
        status = "ALERTA"
        alerta_termux(f"Espaço crítico: Apenas {livre}% livre ({livre_gb} GB)", "notification")
    else:
        status = "SUCCESS"
    registrar_log(status, detalhes)
    print(f"Métricas de disco salvas. Livre: {livre}%")

if __name__ == "__main__":
    main()
