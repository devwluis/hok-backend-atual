# N8N Reiniciar Crashed

## Ação

```bash
docker service update --force n8n_n8n_editor 2>&1 || docker restart $(docker ps -aq --filter status=exited --filter name=n8n) 2>&1 && echo '✅ N8N reiniciado'
```
