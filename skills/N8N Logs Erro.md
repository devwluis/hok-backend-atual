# N8N Logs Erro

## Ação

```bash
docker logs $(docker ps -q --filter name=n8n | head -1) --tail 50 2>&1
```
