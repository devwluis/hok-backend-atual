# Ping Serviços

## Ação

```bash
curl -s --max-time 5 https://api.groq.com/openai/v1/models -H 'Authorization: Bearer test' | python3 -c "import json,sys; d=json.load(sys.stdin); print('Groq:', 'online' if 'error' in d else d)" 2>/dev/null || echo 'Groq: verificar chave'
```
