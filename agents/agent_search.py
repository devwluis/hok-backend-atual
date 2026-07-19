#!/data/data/com.termux/files/usr/bin/python3
"""
agent_search.py - Agente de Busca e Aprendizado Online (Hokmá v14)
Usa o ddgr para buscar na web, extrai o texto limpo da melhor página
e grava na tabela 'memories' do SQLite para aprendizado semântico (RAG).
"""
import os
import json
import sqlite3
import subprocess
import urllib.request
import re

DB_PATH = os.path.expanduser("~/ecossistema/backend/memory.db")

def limpar_html(html):
    """Remove scripts, folhas de estilo e tags HTML retornando apenas texto limpo."""
    html = re.sub(r'<(script|style).*?>([\s\S]*?)</\1>', '', html)
    texto = re.sub(r'<[^>]+>', ' ', html)
    texto = re.sub(r'\s+', ' ', texto)
    return texto.strip()

def buscar_web(query):
    """Realiza pesquisa usando o ddgr e retorna JSON estruturado."""
    try:
        cmd = ["ddgr", "--json", "-n", "3", query]
        out = subprocess.check_output(cmd, timeout=12).decode("utf-8")
        return json.loads(out)
    except Exception as e:
        print(f"Erro ao buscar no ddgr: {e}")
        return []

def ler_pagina(url):
    """Faz o download e limpa a página HTML de destino."""
    req = urllib.request.Request(
        url, 
        headers={'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'}
    )
    try:
        with urllib.request.urlopen(req, timeout=12) as r:
            html = r.read().decode("utf-8", errors="ignore")
            return limpar_html(html)
    except Exception as e:
        return f"Erro ao raspar a pagina: {e}"

def salvar_aprendizado(chave, conteudo):
    """Cunha o novo conhecimento na tabela memories (RAG) do SQLite."""
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
    
    # Grava o conhecimento aprendido de forma permanente
    cursor.execute("INSERT OR REPLACE INTO memories (key, value) VALUES (?, ?)", (chave, conteudo))
    
    # Registra no log do sistema
    cursor.execute("INSERT INTO logs (event, level, source) VALUES (?, ?, ?)",
        (f"Módulo RAG: Aprendizado consolidado para a chave: {chave}", "SUCCESS", "agent_search"))
        
    conn.commit()
    conn.close()

def main(query):
    print("==================================================")
    print(f"🔍 [BUSCA] Pesquisando na Web por: {query}")
    print("==================================================")
    
    resultados = buscar_web(query)
    if not resultados:
        print("❌ Nenhum resultado relevante retornado.")
        return
        
    # Extrai o primeiro e melhor link
    top = resultados[0]
    url = top["url"]
    titulo = top["title"]
    resumo = top["abstract"]
    
    print(f"🔗 [LINK] Encontrado: {titulo}")
    print(f"🌐 [URL]  {url}")
    print(f"📄 [PRE-VIEW] {resumo}\n")
    
    print("📥 [RASPAGEM] Extraindo o texto completo da página...")
    texto_completo = ler_pagina(url)
    
    # Limita o tamanho do texto para 2000 caracteres para evitar estourar o contexto da IA
    if len(texto_completo) > 2000:
        texto_completo = texto_completo[:2000] + "..."
        
    # Formata o bloco de conhecimento estruturado
    conhecimento = f"Assunto pesquisado: {query} | Fonte: {titulo} ({url}) | Fatos: {texto_completo}"
    
    # Define a chave de busca limpa (removendo caracteres especiais)
    chave_aprendida = "aprendizado_" + re.sub(r'[^a-zA-Z0-9]', '_', query.lower())
    
    # Salvar na base de conhecimento
    salvar_aprendizado(chave_aprendida, conhecimento)
    print(f"\n🧠 [SUCESSO] Conhecimento cunhado sob a chave: {chave_aprendida}!")
    print("==================================================")

if __name__ == "__main__":
    import sys
    if len(sys.argv) > 1:
        main(" ".join(sys.argv[1:]))
    else:
        print("Uso: python3 agent_search.py <sua_pesquisa>")
