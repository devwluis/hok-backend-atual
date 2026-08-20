package main

import (
	"encoding/json"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// propagateActiveModelToMotors grava o modelo ativo nos arquivos de config dos
// motores que leem de disco: ~/.claude/settings.json (Claude Code) e
// ~/.opencode/opencode.json (OpenCode CLI). O Hermes le o modelo em runtime
// via getActiveModel(), entao nao precisa de arquivo. Falhas sao logadas e
// nunca quebram o fluxo de selecao de modelo.
func propagateActiveModelToMotors(model string) {
	propagateToClaudeSettings(model)
	propagateToOpenCodeConfig(model)
}

func propagateToClaudeSettings(model string) {
	// O CLI do claude mescla o bloco env do settings.json POR CIMA do ambiente
	// do processo — por isso o valor gravado aqui precisa ser o id aceito pelo
	// proxy OpenRouter (sem sufixos de tier do catalogo, ex: -free).
	model = normalizeModelSlugForAPI(model)
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("⚠️ propagate: sem home (%v)", err)
		return
	}
	writeClaudeSettings(filepath.Join(home, ".claude", "settings.json"), model)
	// FIX 20/08 (root): o fluxo aprovado do claude roda via runuser como
	// usuario dedicado hokma-agent, cujo settings.json tambem precisa do
	// modelo atualizado (e da chave bypassPermissionsModeAccepted, que o
	// merge abaixo preserva).
	if u, uerr := user.Lookup(claudeAgentUser); uerr == nil && u.HomeDir != "" {
		writeClaudeSettings(filepath.Join(u.HomeDir, ".claude", "settings.json"), model)
	}
}

// writeClaudeSettings faz merge do bloco env num settings.json existente
// (preservando demais chaves, ex: bypassPermissionsModeAccepted) e corrige o
// ownership para o dono real do arquivo (o backend roda como root).
func writeClaudeSettings(path string, model string) {
	raw, err := os.ReadFile(path)
	cfg := map[string]interface{}{}
	if err == nil {
		if json.Unmarshal(raw, &cfg) != nil {
			cfg = map[string]interface{}{}
		}
	}
	env, _ := cfg["env"].(map[string]interface{})
	if env == nil {
		env = map[string]interface{}{}
	}
	env["ANTHROPIC_MODEL"] = model
	env["ANTHROPIC_SMALL_FAST_MODEL"] = model
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = model
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = model
	env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = model
	cfg["env"] = env
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Printf("⚠️ propagate claude: marshal (%v)", err)
		return
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		log.Printf("⚠️ propagate claude: write (%v)", err)
		return
	}
	if fi, serr := os.Stat(path); serr == nil && fi.Mode().Perm() != 0o600 {
		os.Chmod(path, 0o600)
	}
	if u, uerr := user.Lookup(claudeAgentUser); uerr == nil {
		if uid, e1 := strconv.Atoi(u.Uid); e1 == nil {
			if gid, e2 := strconv.Atoi(u.Gid); e2 == nil {
				os.Chown(path, uid, gid)
			}
		}
	}
	log.Printf("✅ propagate: %s → %s", path, model)
}

func propagateToOpenCodeConfig(model string) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("⚠️ propagate: sem home (%v)", err)
		return
	}
	dir := filepath.Join(home, ".opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("⚠️ propagate opencode: mkdir (%v)", err)
		return
	}
	path := filepath.Join(dir, "opencode.json")
	raw, err := os.ReadFile(path)
	cfg := map[string]interface{}{}
	if err == nil {
		if json.Unmarshal(raw, &cfg) != nil {
			cfg = map[string]interface{}{}
		}
	}
	cfg["model"] = opencodeModelID(model)
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Printf("⚠️ propagate opencode: marshal (%v)", err)
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		log.Printf("⚠️ propagate opencode: write (%v)", err)
		return
	}
	log.Printf("✅ propagate: ~/.opencode/opencode.json → %s", opencodeModelID(model))
}