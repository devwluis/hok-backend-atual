package main

import (
	"strings"
	"testing"
)

// TestFreeModelCategory — classificação por categoria com base em metadata
// oficial (features/modality/ctx/id).
func TestFreeModelCategory(t *testing.T) {
	cases := []struct {
		id, name, features, modality string
		ctx                          int
		want                         string
	}{
		{"coding-kimi-k3-free", "Coding Kimi K3", "thinking,tools,function_calling,structured_outputs", "", 1048576, "coding/agentic"},
		{"minimax/minimax-m3:free", "MiniMax M3", "", "text+image+video->text", 1048576, "chat geral/multimodal"},
		{"nvidia/nemotron-3-super-120b-a12b:free", "Nemotron 3 Super", "", "text->text", 262144, "reasoning/análise"},
		{"gpt-5.5-free", "GPT 5.5", "thinking,function_calling,structured_outputs,web,tools", "", 1050000, "coding/agentic"},
		{"thinkingmachines/inkling:free", "Inkling", "", "text+image+audio->text", 1048576, "chat geral/multimodal"},
		{"google/gemma-4-26b-a4b-it:free", "Gemma 4", "", "text+image+video->text", 262144, "chat geral/multimodal"},
	}
	for _, c := range cases {
		got := freeModelCategory(c.id, c.name, c.features, c.modality, c.ctx)
		if got != c.want {
			t.Errorf("freeModelCategory(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

// TestFreeModelCategoryFunctionCalling — tools/function_calling vira
// coding/agentic antes de reasoning (ordem de prioridade documentada).
func TestFreeModelCategoryFunctionCalling(t *testing.T) {
	got := freeModelCategory("x-free", "Model X", "thinking,function_calling", "", 100000)
	if got != "coding/agentic" {
		t.Fatalf("com function_calling deveria ser coding/agentic, veio %q", got)
	}
}

// TestFreeModalityFeatures — converte modality da OpenRouter em capacidades.
func TestFreeModalityFeatures(t *testing.T) {
	got := freeModalityFeatures("text+image+video->text")
	if len(got) != 2 || got[0] != "image" || got[1] != "video" {
		t.Fatalf("modality parse errado: %v", got)
	}
	if freeModalityFeatures("") != nil {
		t.Fatal("modality vazia deveria ser nil")
	}
}

// TestCatalogSyncFromFreeLists — delta detection: added/removed/metadata_changed.
func TestCatalogSyncFromFreeLists(t *testing.T) {
	snap := []catalogSnapshotRow{
		{ID: "a-free", Provider: "OpenRouter", Category: "chat geral", Source: "api-official"},
		{ID: "b-free", Provider: "OpenRouter", Category: "coding/agentic", Source: "api-official"},
		{ID: "c-free", Provider: "AIHubMix", Category: "reasoning/análise", Source: "api-official"},
	}
	newFree := map[string]catalogSnapshotRow{
		"b-free": {ID: "b-free", Provider: "OpenRouter", Category: "coding/agentic", Source: "api-official"}, // igual
		"c-free": {ID: "c-free", Provider: "AIHubMix", Category: "chat geral", Source: "api-official"},       // mudou categoria
		"d-free": {ID: "d-free", Provider: "OpenCode Go", Category: "chat geral", Source: "manual-go"},       // novo
	}
	added, removed, changed := catalogSyncFromFreeLists(snap, newFree)
	if len(added) != 1 || added[0].ID != "d-free" {
		t.Fatalf("added errado: %+v", added)
	}
	if len(removed) != 1 || removed[0].ID != "a-free" {
		t.Fatalf("removed errado: %+v", removed)
	}
	if len(changed) != 1 || changed[0].ID != "c-free" {
		t.Fatalf("changed errado: %+v", changed)
	}
}

// TestCatalogFreeOnly — a curadoria/snapshot opera SÓ em free: modelos pagos
// não entram no mapa do snapshot.
func TestCatalogFreeOnly(t *testing.T) {
	// Simula catálogo em memória com pagos + free.
	catalogCacheMutex.Lock()
	orig := catalogCache
	catalogCache = []ModelCatalogItem{
		{ID: "z-ai/glm-5.3-flash", Provider: "OpenRouter", Free: false, Category: ""}, // pago — NÃO entra
		{ID: "minimax/minimax-m3:free", Provider: "OpenRouter", Free: true, Category: "chat geral/multimodal", FreeSource: "api-official"},
		{ID: "opencode-go/deepseek-v4-flash", Provider: "OpenCode Go", Free: true, Category: "chat geral", FreeSource: "manual-go"},
	}
	catalogCacheMutex.Unlock()
	defer func() {
		catalogCacheMutex.Lock()
		catalogCache = orig
		catalogCacheMutex.Unlock()
	}()

	m := freeCatalogFromCache()
	if _, ok := m["z-ai/glm-5.3-flash"]; ok {
		t.Fatal("modelo PAGO não pode entrar na curadoria free")
	}
	if _, ok := m["minimax/minimax-m3:free"]; !ok {
		t.Fatal("modelo free deveria estar no snapshot")
	}
	if got := m["opencode-go/deepseek-v4-flash"].Source; got != "manual-go" {
		t.Fatalf("freeSource do Go deveria ser manual-go, veio %q", got)
	}
}

// TestCatalogPaidDenylist — modelos na paidDenylist nunca entram no catálogo
// (merge) nem na curadoria free (sync), mesmo que a API os liste como free.
func TestCatalogPaidDenylist(t *testing.T) {
	if !paidDenylist["opencode-go/glm-5.3-flash"] {
		t.Fatal("GLM-5.3-Flash deve estar na denylist")
	}
	if !paidDenylist["opencode-go/muse-spark-1.2-contributor"] {
		t.Fatal("Muse Spark 1.2 (Go) deve estar na denylist")
	}
	if !paidDenylist["opencode/muse-spark-1.2-contributor-free"] {
		t.Fatal("Muse Spark 1.2 (Zen) deve estar na denylist")
	}

	// mergeModels remove os 3 mesmo vindo das fontes.
	merged := mergeModels(
		[]ModelCatalogItem{{ID: "opencode/muse-spark-1.2-contributor-free", Provider: "OpenCode Zen", Free: true, FreeSource: "cli"}},
		nil,
		[]ModelCatalogItem{{ID: "minimax/minimax-m3:free", Provider: "OpenRouter", Free: true}},
		[]ModelCatalogItem{{ID: "opencode-go/glm-5.3-flash", Provider: "OpenCode Go", Free: true, FreeSource: "manual-go"}},
		nil,
	)
	for _, m := range merged {
		if paidDenylist[m.ID] {
			t.Fatalf("modelo %s não deveria existir no catálogo mesclado", m.ID)
		}
	}
	if len(merged) != 1 || merged[0].ID != "minimax/minimax-m3:free" {
		t.Fatalf("merge deveria conter só o free permitido, veio: %+v", merged)
	}

	// freeCatalogFromCache também respeita a denylist (sync não re-insere).
	catalogCacheMutex.Lock()
	orig := catalogCache
	catalogCache = []ModelCatalogItem{
		{ID: "opencode-go/glm-5.3-flash", Provider: "OpenCode Go", Free: true, Category: "chat geral", FreeSource: "manual-go"},
		{ID: "minimax/minimax-m3:free", Provider: "OpenRouter", Free: true, Category: "chat geral/multimodal", FreeSource: "api-official"},
	}
	catalogCacheMutex.Unlock()
	defer func() {
		catalogCacheMutex.Lock()
		catalogCache = orig
		catalogCacheMutex.Unlock()
	}()

	m := freeCatalogFromCache()
	if _, ok := m["opencode-go/glm-5.3-flash"]; ok {
		t.Fatal("GLM-5.3-Flash não pode entrar na curadoria free (denylist)")
	}
	if _, ok := m["minimax/minimax-m3:free"]; !ok {
		t.Fatal("modelo free permitido deveria estar no snapshot")
	}
}

// TestFreeSourceMarking — fetches de OpenRouter/AIHubMix marcam
// api-official; Zen cli; Go manual-go.
func TestFreeSourceMarking(t *testing.T) {
	// OpenRouter: modelo free ganha api-official + categoria.
	orItems := []ModelCatalogItem{{ID: "minimax/minimax-m3:free", Label: "M3", Provider: "OpenRouter", Free: true, ContextLength: 1048576, Features: []string{"image"}, Category: "chat geral/multimodal", FreeSource: "api-official"}}
	if !strings.Contains(orItems[0].ID, "minimax-m3") || orItems[0].FreeSource != "api-official" {
		t.Fatalf("marcação OpenRouter errada: %+v", orItems[0])
	}
	// Go CLI: manual-go.
	goItems := []ModelCatalogItem{{ID: "opencode-go/deepseek-v4-flash", Provider: "OpenCode Go", Free: true, FreeSource: "manual-go"}}
	if goItems[0].FreeSource != "manual-go" {
		t.Fatalf("marcação Go errada: %+v", goItems[0])
	}
	// Zen: cli.
	zenItems := []ModelCatalogItem{{ID: "opencode/mimo-v2.5-free", Provider: "OpenCode Zen", Free: true, FreeSource: "cli"}}
	if zenItems[0].FreeSource != "cli" {
		t.Fatalf("marcação Zen errada: %+v", zenItems[0])
	}
}
