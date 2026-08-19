# N8N Workflows Ativos

## Ação

```bash
curl -s http://localhost:5678/api/v1/workflows?active=true 2>/dev/null | python3 -m json.tool 2>/dev/null || echo 'N8N: acesse via browser'
```
