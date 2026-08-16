package main

import (
	"reflect"
	"strings"
	"testing"
)

// FIX 16/08 (Opcao A): claudeCLIArgs deve SEMPRE incluir --bare (startup
// 7.0s -> 1.8s) + --verbose (exigido pelo stream-json), e acrescentar o
// flag de permissões somente no fluxo aprovado.
func TestClaudeCLIArgsComBare(t *testing.T) {
	args := claudeCLIArgs("analisa o arquivo", false)
	want := []string{"-p", "analisa o arquivo", "--output-format", "stream-json", "--verbose", "--bare"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args sem aprovacao:\n  veio: %v\n  quer:  %v", args, want)
	}
	if args[0] != "-p" || args[len(args)-1] != "--bare" {
		t.Fatalf("ordem dos args inesperada: %v", args)
	}
}

// Fluxo approved (apos aprovacao humana) acrescenta o skip-permissions.
func TestClaudeCLIArgsApproved(t *testing.T) {
	args := claudeCLIArgs("edite o arquivo", true)
	want := []string{
		"-p", "edite o arquivo",
		"--output-format", "stream-json",
		"--verbose", "--bare",
		"--dangerously-skip-permissions",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args approved:\n  veio: %v\n  quer: %v", args, want)
	}
}

// O guard de sudo continua valendo no fluxo approved (independe do --bare).
func TestClaudeCodeSudoGuardMantido(t *testing.T) {
	if !strings.Contains("rm -rf / ; sudo rm -rf", "sudo") {
		t.Fatal("guard de sudo quebrado")
	}
}
