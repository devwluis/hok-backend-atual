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
	sanitizeOpenCodeConfig(cfg)
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

// sanitizeOpenCodeConfig normaliza uma config do OpenCode lida de disco antes
// de reescrever (propagate faz merge preservando chaves existentes — sem este
// saneador, uma config poluída com apiKey:null ou bloco mcp de schema antigo
// seria propagada de volta e quebraria o opencode com "Configuration is
// invalid"). Remove:
//   - provider.<id>.options.apiKey quando null/vazio (o schema exige string
//     OU o campo omitido — nunca null explícito);
//   - bloco mcp no formato ANTIGO (enabled/command/type soltos no topo),
//     reescrevendo para o formato atual: objeto nomeado por servidor com
//     { "type": "local"|"remote", ... }.
func sanitizeOpenCodeConfig(cfg map[string]interface{}) {
	// 1) provider.<id>.options.apiKey null → remove a chave (ou o bloco options
	//    inteiro se ficar vazio).
	if prov, ok := cfg["provider"].(map[string]interface{}); ok {
		for _, pv := range prov {
			opts, ok := pv.(map[string]interface{})
			if !ok {
				continue
			}
			if v, has := opts["options"]; has {
				if om, ok := v.(map[string]interface{}); ok {
					if av, hasAK := om["apiKey"]; hasAK && av == nil {
						delete(om, "apiKey")
					}
					if len(om) == 0 {
						delete(opts, "options")
					}
				}
			}
		}
	}
	// 2) mcp legado: { enabled, command, type } no topo → formato nomeado.
	if mcp, ok := cfg["mcp"].(map[string]interface{}); ok {
		if _, hasLegacy := mcp["enabled"]; hasLegacy {
			command, _ := mcp["command"].([]interface{})
			typ, _ := mcp["type"].(string)
			if typ == "" {
				typ = "local"
			}
			server := map[string]interface{}{}
			if len(command) > 0 {
				server["command"] = command
			}
			server["type"] = typ
			cfg["mcp"] = map[string]interface{}{
				"playwright": server, // nome preservado do servidor legado
			}
		}
	}
}