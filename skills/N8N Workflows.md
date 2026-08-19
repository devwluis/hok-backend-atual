# N8N Workflows

## Ação

```bash
curl -s http://localhost:5678/api/v1/workflows -H 'accept: application/json' 2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); [print(w['name'], '| ativo:', w['active']) for w in d.get('data',[])]" 2>/dev/null || echo 'N8N: verifique autenticação'
```
