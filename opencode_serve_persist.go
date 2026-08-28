package main

// opencode_serve_persist.go — persistência da sessão do opencode serve por
// conversa (Fase 3).
//
// Reaproveita a tabela `session_mode` desenhada no adendo de 24/08
// (tenant_id, user_id, conv_id, mode, autonomous_budget, set_by,
// updated_at), estendida com `opencode_session_id` para guardar o
// session_id do `opencode serve` associado a cada conv_id. Chave primária
// (tenant_id, user_id, conv_id) garante UMA sessão por conversa — reabrir a
// mesma conv_id reaproveita a sessão existente em vez de criar duplicada.

import (
	"log"
)

// getOpenCodeServeSessionID devolve o session_id persistido para a conversa
// ("" se ainda não existir linha).
func getOpenCodeServeSessionID(convID, tenantID, userID string) string {
	if convID == "" || tenantID == "" || userID == "" {
		return ""
	}
	var sid string
	err := db.QueryRow(
		`SELECT opencode_session_id FROM session_mode
		 WHERE tenant_id = ? AND user_id = ? AND conv_id = ?`,
		tenantID, userID, convID,
	).Scan(&sid)
	if err != nil {
		return ""
	}
	return sid
}

// setOpenCodeServeSessionID grava/atualiza o session_id da conversa
// (INSERT ... ON CONFLICT mantém uma linha por conv_id).
func setOpenCodeServeSessionID(convID, tenantID, userID, sessionID string) {
	if convID == "" || tenantID == "" || userID == "" || sessionID == "" {
		return
	}
	sqliteExecParams(
		`INSERT INTO session_mode (tenant_id, user_id, conv_id, mode, set_by, opencode_session_id, updated_at)
		 VALUES (?, ?, ?, 'plan', 'opencode_serve', ?, unixepoch())
		 ON CONFLICT(tenant_id, user_id, conv_id) DO UPDATE SET
		   opencode_session_id = excluded.opencode_session_id,
		   set_by             = 'opencode_serve',
		   updated_at         = unixepoch()`,
		tenantID, userID, convID, sessionID,
	)
}

// clearOpenCodeServeSessionID remove o mapeamento da conversa (sessão zumbi:
// a próxima mensagem cria sessão nova via getOrCreate).
func clearOpenCodeServeSessionID(convID, tenantID, userID string) {
	if convID == "" || tenantID == "" || userID == "" {
		return
	}
	sqliteExecParams(
		`DELETE FROM session_mode WHERE tenant_id = ? AND user_id = ? AND conv_id = ?`,
		tenantID, userID, convID,
	)
}

// getOrCreateOpenCodeServeSession — núcleo da persistência: devolve a sessão
// existente da conv_id (reused=true) ou cria uma nova e persiste
// (reused=false). Se a sessão persistida não existir mais no servidor
// (por exemplo, servidor reiniciado sem storage), cria outra no lugar.
func getOrCreateOpenCodeServeSession(convID, tenantID, userID, title string, c *opencodeServeClient) (sessionID string, reused bool, err error) {
	existing := getOpenCodeServeSessionID(convID, tenantID, userID)
	if existing != "" {
		if s, gerr := c.getSession(existing); gerr == nil && s != nil && s.ID == existing {
			return existing, true, nil
		}
		log.Printf("opencode_serve: sessao persistida %s nao existe mais no servidor — recriando para conv %s", existing, convID)
	}
	s, cerr := c.createSession(title)
	if cerr != nil {
		return "", false, cerr
	}
	setOpenCodeServeSessionID(convID, tenantID, userID, s.ID)
	return s.ID, false, nil
}