package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RestrictiveConfig — config OpenCode gerado pelo wrapper com blocklist de
// comandos perigosos. Sobrescreve o config do user durante a sessão via
// XDG_CONFIG_HOME (opencode respeita XDG_CONFIG_HOME).
type RestrictiveConfig struct {
	Schema     string                      `json:"$schema"`
	Permission map[string]any              `json:"permission"`
	Model      string                      `json:"model,omitempty"`
	Username   string                      `json:"username,omitempty"`
}

// DefaultDangerousPatterns — comandos bash que NUNCA devem rodar autônomo.
// Lista conservadora; ajustes via PR são bem-vindos.
var DefaultDangerousPatterns = []string{
	`rm\s+-rf\s+/`,                   // rm -rf /
	`rm\s+-rf\s+~`,                   // rm -rf ~
	`rm\s+-rf\s+\*`,                  // rm -rf *
	`rm\s+-rf\s+\.[\w]+`,             // rm -rf .algo
	`sudo\s+`,                        // qualquer sudo
	`dd\s+if=`,                       // dd if=...
	`mkfs`,                           // formatar partição
	`fdisk`,                          // mexer em partição
	`curl\s+.*\|\s*(sh|bash)`,        // curl | sh
	`wget\s+.*\|\s*(sh|bash)`,        // wget | sh
	`chmod\s+-R\s+777`,               // chmod -R 777
	`>\s*/dev/sd`,                    // redirecionar pra disco
	`mkfs\.ext`,                      // mkfs.ext*
	`shutdown`,                       // shutdown
	`reboot`,                         // reboot
	`halt`,                           // halt
	`poweroff`,                       // poweroff
	`init\s+0`,                       // init 0
	`systemctl\s+(stop|disable|mask)\s+hokma`,
	`systemctl\s+(stop|disable|mask)\s+hok-terminal`,
	`systemctl\s+(stop|disable|mask)\s+opencode-serve`,
	`rm\s+-rf\s+/root/hokma`,
	`rm\s+-rf\s+/root/.opencode`,
	`rm\s+-rf\s+/root/.config/opencode`,
	`rm\s+-rf\s+/root/.local/share/opencode`,
}

// SensitivePaths — paths onde edit/read/write são negados.
var SensitivePaths = []string{
	"/root/.ssh/**",
	"/root/.opencode/**",
	"/root/.config/opencode/**",
	"/root/.local/share/opencode/**",
	"/etc/passwd",
	"/etc/shadow",
	"/etc/sudoers",
	"/root/.bashrc",
	"/root/.zshrc",
	"/root/.profile",
}

// GenerateRestrictiveConfig — gera JSON do config restritivo a partir do
// config atual do user (preserva model/username).
func GenerateRestrictiveConfig(userConfigPath string) (*RestrictiveConfig, error) {
	rc := &RestrictiveConfig{
		Schema: "https://opencode.ai/config.json",
		Permission: map[string]any{
			"bash": map[string]string{
				"*": "ask",
			},
			"edit": map[string]string{
				"*": "ask",
			},
			"webfetch": "allow",
		},
	}

	// Lê config do user (best-effort)
	if userConfigPath != "" {
		if data, err := os.ReadFile(userConfigPath); err == nil {
			var uc map[string]interface{}
			if err := json.Unmarshal(data, &uc); err == nil {
				if m, ok := uc["model"].(string); ok {
					rc.Model = m
				}
				if u, ok := uc["username"].(string); ok {
					rc.Username = u
				}
			}
		}
	}

	// Adiciona blocklist bash
	if bashPerm, ok := rc.Permission["bash"].(map[string]string); ok {
		for _, p := range DefaultDangerousPatterns {
			bashPerm[p] = "deny"
		}
	}
	// Adiciona deny paths em edit
	if editPerm, ok := rc.Permission["edit"].(map[string]string); ok {
		for _, p := range SensitivePaths {
			editPerm[p] = "deny"
		}
	}

	return rc, nil
}

// WriteRestrictiveConfig — escreve o config em <dir>/opencode.json.
// Cria <dir>/opencode/ se precisar.
func WriteRestrictiveConfig(dir string, rc *RestrictiveConfig) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	cfgPath := filepath.Join(dir, "opencode.json")
	data, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", cfgPath, err)
	}
	return cfgPath, nil
}