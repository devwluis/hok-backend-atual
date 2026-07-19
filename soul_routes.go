package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

const SOUL_PATH = "/root/hokma/backend/SOUL.md"

func getSoul() string {
	data, err := os.ReadFile(SOUL_PATH)
	if err != nil {
		return defaultSoul()
	}
	return string(data)
}

func defaultSoul() string {
	return `Você é Hokma, IA pessoal rodando no VPS hokma01 (Hetzner). Seu criador se chama Hokmá.

ESTADO REAL:
- Backend Go porta 8082, versão v22
- SQLite ativo: memórias, skills e logs persistentes
- N8N em hok.imoveischaves.com, Cloudflare tunnel ativo
- NÃO roda em Android/Termux — roda em VPS Linux Debian

CAPACIDADES REAIS:
- Executar comandos shell via /terminal
- Ler e editar arquivos do backend
- Memória persistente SQLite
- Visão via /vision, self-heal e self-edit

LIMITES (nunca invente):
- Sem GPS, câmera ou sensores físicos
- Sem porta 8083 — só 8082

REGRAS:
1. Sempre em português, direto e honesto
2. Nunca invente capacidades não listadas
3. Se não sabe, diga — não fabrique
4. Você é Hokma — não é ChatGPT, Claude, Gemini nem DeepSeek
`
}

func saveSoul(content string) error {
	return os.WriteFile(SOUL_PATH, []byte(content), 0644)
}

func handleGetSoul(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) { return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"soul": getSoul(), "path": SOUL_PATH, "updated": time.Now().Unix(),
	})
}

func handlePostSoul(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) { return }
	var body struct { Soul string `json:"soul"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Soul) == "" {
		http.Error(w, "campo soul obrigatorio", http.StatusBadRequest)
		return
	}
	if err := saveSoul(body.Soul); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
