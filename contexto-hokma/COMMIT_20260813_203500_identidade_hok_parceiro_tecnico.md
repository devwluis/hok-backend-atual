# COMMIT 2026-08-13 20:35 — Identidade Hok: parceiro técnico (dev sênior PT-BR)

## Objetivo
Trocar a persona do HOK de "assistente amigável" para **parceiro técnico do Hokmá** no desenvolvimento do HOK OS: dev sênior, PT-BR com termos técnicos em inglês, direto, comandos em blocos prontos pro Termius, risco/trade-off antes de executar, sem recapitule, investigação guiada por dado (não suposição).

## Alterações (backend commit 636daff, main)
- `smart_chat.go` → `smartChatSystemPrompt()` reescrito com a identidade completa (Hok/parceiro técnico, HOK OS, n8n, Imóveis Chaves; seção Como conversar + O que evitar). SOUL.md continua injetado junto.
- `SOUL.md` → seção PERSONA alinhada com a mesma identidade (removeu tom "amigável/humano", adicionou dev sênior, Termius, trade-off antes de executar, não confirmar sucesso sem log).

## Validação (E2E real)
- Build OK (`go build`), serviço ativo após deploy.
- `POST /chat/smart` "Oi, beleza?" → "Beleza, tranquilo. O que você tá precisando hoje?" (mode:chat, natural, sem gate).
- `POST /chat/smart` "Como eu faco um deploy rapido do backend?" → resposta em cenários + blocos de comando copiar/colar (estilo dev).

## Pendências
- Multimodal (visão/áudio/arquivos): conversa aberta, ver COMMIT anterior. DeepSeek v4 flash é texto-only; Gemini 2.5 Flash e OR qwen2.5-vl testados OK; ASR segue em Groq Whisper (única opção grátis funcional); handleSmartChatWithFiles é stub.
