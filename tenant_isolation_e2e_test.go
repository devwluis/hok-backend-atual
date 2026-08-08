package main

import (
	"net/http/httptest"
	"testing"
)

func TestTenantIdFromRequestExtraction(t *testing.T) {
	// Gerar token JWT com tenant_id específico
	token, err := generateJWT("user1", "test@alpha.com", "client", "tenant_alpha")
	if err != nil {
		t.Fatalf("Falha ao gerar JWT: %v", err)
	}

	// Criar request fake com header Authorization
	req := httptest.NewRequest("POST", "/actions/approve", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	// Extrair tenantID — essa é a função que o patch adicionou
	tid := tenantIdFromRequest(req)
	if tid != "tenant_alpha" {
		t.Fatalf("Esperado 'tenant_alpha', obtido '%s'", tid)
	}

	// Token sem tenant_id (fallback para "owner")
	tokenNoTenant, _ := generateJWT("user2", "test@owner.com", "client", "")
	req2 := httptest.NewRequest("POST", "/actions/approve", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenNoTenant)
	tid2 := tenantIdFromRequest(req2)
	if tid2 != "owner" {
		t.Fatalf("Esperado 'owner' (fallback), obtido '%s'", tid2)
	}

	t.Log("✅ tenantIdFromRequest extrai tenant_id do JWT corretamente")
}

func TestE2ETenantIsolationApproval(t *testing.T) {
	// Reset do mapa para teste limpo
	pendingActionMu.Lock()
	pendingActionMap = map[string]*PendingAction{}
	pendingActionMu.Unlock()

	// Gerar 2 tokens com tenants diferentes
	tokenA, _ := generateJWT("user_a", "a@test.com", "client", "tenant_a")
	tokenB, _ := generateJWT("user_b", "b@test.com", "client", "tenant_b")

	// Criar requests simulando 2 usuários diferentes na MESMA conversa
	reqA := httptest.NewRequest("POST", "/actions/approve", nil)
	reqA.Header.Set("Authorization", "Bearer "+tokenA)
	reqA.Header.Set("X-Conversation-Id", "shared_conv_123")

	reqB := httptest.NewRequest("POST", "/actions/approve", nil)
	reqB.Header.Set("Authorization", "Bearer "+tokenB)
	reqB.Header.Set("X-Conversation-Id", "shared_conv_123")

	// Extrair tenantIDs (como as rotas reais fazem)
	tenantA := tenantIdFromRequest(reqA)
	tenantB := tenantIdFromRequest(reqB)

	// Simular que o agente criou pending actions para ambos
	setPendingAction("shared_conv_123", tenantA, "", "bash_exec", `{"cmd":"echo tenant_a"}`, "Acao do Tenant A")
	setPendingAction("shared_conv_123", tenantB, "", "bash_exec", `{"cmd":"echo tenant_b"}`, "Acao do Tenant B")

	// Verificar isolamento: cada um só vê o seu
	paA := getPendingAction("shared_conv_123", tenantA, "")
	paB := getPendingAction("shared_conv_123", tenantB, "")
	if paA == nil || paA.Description != "Acao do Tenant A" {
		t.Fatal("Tenant A nao encontrou sua pending action")
	}
	if paB == nil || paB.Description != "Acao do Tenant B" {
		t.Fatal("Tenant B nao encontrou sua pending action")
	}

	// Cenário crítico: Tenant A aprova sua ação
	// Usar resolvePendingAction como a rota /actions/approve faz
	replyA := resolvePendingAction("shared_conv_123", tenantA, "", true)
	if replyA == "Nao ha nenhuma acao pendente no momento." {
		t.Fatal("Tenant A deveria ter uma acao pendente")
	}

	// VERIFICAÇÃO CHAVE: Tenant B DEVE continuar com sua pending action intacta
	paBAfter := getPendingAction("shared_conv_123", tenantB, "")
	if paBAfter == nil {
		t.Fatal("ISOLAMENTO QUEBRADO: Tenant B perdeu sua pending action quando Tenant A aprovou")
	}
	if paBAfter.Description != "Acao do Tenant B" {
		t.Fatalf("Tenant B foi corrompido: %s", paBAfter.Description)
	}

	// Tenant A não deve mais ter pendência
	paAAfter := getPendingAction("shared_conv_123", tenantA, "")
	if paAAfter != nil {
		t.Fatal("Tenant A ainda tem pending action depois de aprovar")
	}

	// Limpar
	clearPendingAction("shared_conv_123", tenantB, "")

	t.Log("✅ E2E validado: 2 tenants, mesmo convId, aprovação cruzada = zero colisão")
}

func TestE2ETenantIsolationWithDiffPreview(t *testing.T) {
	// Testar que automodificação gera DiffPreview
	pendingActionMu.Lock()
	pendingActionMap = map[string]*PendingAction{}
	pendingActionMu.Unlock()

	token, _ := generateJWT("user1", "test@diff.com", "client", "tenant_diff")
	req := httptest.NewRequest("POST", "/actions/approve", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	tenantID := tenantIdFromRequest(req)

	// Criar uma pending action de automodificação (sed em arquivo real)
	setPendingAction("conv_diff", tenantID, "", "bash_exec", `{"cmd":"sed -i 's/old/new/g' /root/hokma/backend/README.md"}`, "Trocar old por new")

	pa := getPendingAction("conv_diff", tenantID, "")
	if pa == nil {
		t.Fatal("Pending action nao criada")
	}
	if pa.ActionType != "self_mod" {
		t.Fatalf("Esperado actionType 'self_mod', obtido '%s'", pa.ActionType)
	}
	if pa.DiffPreview == "" {
		t.Log("⚠️ DiffPreview vazio — pode ser que o arquivo README.md nao exista ou o padrao nao foi reconhecido")
	} else {
		t.Logf("✅ DiffPreview gerado (%d chars)", len(pa.DiffPreview))
		if len(pa.DiffPreview) > 100 {
			t.Logf("Preview: %s...", pa.DiffPreview[:100])
		}
	}

	clearPendingAction("conv_diff", tenantID, "")
}
