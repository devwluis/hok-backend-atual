# Webhook Teste

## Ação

```bash
curl -s -X POST http://localhost:5678/webhook/hokma-heal -H 'Content-Type: application/json' -d '{"test":true}' 2>/dev/null || echo 'webhook não configurado'
```
