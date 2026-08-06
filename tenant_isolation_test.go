package main

import (
	"sync"
	"testing"
)

func TestTenantIsolation(t *testing.T) {
	// Reset do mapa para teste limpo
	pendingActionMu.Lock()
	pendingActionMap = map[string]*PendingAction{}
	pendingActionMu.Unlock()

	// Tenant A e Tenant B com a MESMA conversationId
	// Isso é o cenário mais perigoso — se a chave não for composta, colidem
	setPendingAction("conv_test_123", "tenant_a", "n8n_create_workflow", `{"name":"wf_a"}`, "Criar workflow A")
	setPendingAction("conv_test_123", "tenant_b", "n8n_create_workflow", `{"name":"wf_b"}`, "Criar workflow B")

	// Verificar isolamento: cada tenant só vê sua própria ação
	paA := getPendingAction("conv_test_123", "tenant_a")
	paB := getPendingAction("conv_test_123", "tenant_b")

	if paA == nil {
		t.Fatal("Tenant A deveria ver sua pending action")
	}
	if paB == nil {
		t.Fatal("Tenant B deveria ver sua pending action")
	}
	if paA.Description != "Criar workflow A" {
		t.Fatalf("Tenant A vendo descrição errada: %s", paA.Description)
	}
	if paB.Description != "Criar workflow B" {
		t.Fatalf("Tenant B vendo descrição errada: %s", paB.Description)
	}

	// Aprovar a do tenant A — tenant B deve continuar com a dele intacta
	resolvePendingAction("conv_test_123", "tenant_a", true)
	paBAfter := getPendingAction("conv_test_123", "tenant_b")
	if paBAfter == nil {
		t.Fatal("Tenant B perdeu sua pending action quando Tenant A aprovou — ISOLAMENTO QUEBRADO")
	}
	if paBAfter.Description != "Criar workflow B" {
		t.Fatalf("Tenant B foi corrompido: %s", paBAfter.Description)
	}

	// Tenant A não deve mais ter pendência
	paAAfter := getPendingAction("conv_test_123", "tenant_a")
	if paAAfter != nil {
		t.Fatal("Tenant A ainda tem pending action depois de aprovar")
	}

	// Limpar
	clearPendingAction("conv_test_123", "tenant_b")
	t.Log("✅ Isolamento por tenant validado: mesmo convId, tenants diferentes, zero colisão")
}

func TestTenantIsolationConcurrent(t *testing.T) {
	pendingActionMu.Lock()
	pendingActionMap = map[string]*PendingAction{}
	pendingActionMu.Unlock()

	var wg sync.WaitGroup
	tenants := []string{"alpha", "beta", "gamma", "delta"}

	// Criar 4 pendências simultâneas
	for _, tenant := range tenants {
		wg.Add(1)
		go func(tid string) {
			defer wg.Done()
			setPendingAction("shared_conv", tid, "bash_exec", `{"cmd":"echo `+tid+`"}`, "Teste "+tid)
		}(tenant)
	}
	wg.Wait()

	// Verificar que cada um só vê o seu
	for _, tenant := range tenants {
		pa := getPendingAction("shared_conv", tenant)
		if pa == nil {
			t.Fatalf("Tenant %s não encontrou sua pending action", tenant)
		}
		if pa.TenantID != tenant {
			t.Fatalf("Tenant %s encontrou pending de outro tenant: %s", tenant, pa.TenantID)
		}
	}

	// Limpar
	for _, tenant := range tenants {
		clearPendingAction("shared_conv", tenant)
	}

	t.Log("✅ Concorrência validada: 4 tenants simultâneos, zero colisão")
}
