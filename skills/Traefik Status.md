# Traefik Status

## Ação

```bash
docker service ls | grep traefik && curl -s http://localhost:8080/api/rawdata 2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); print('routers:', len(d.get('routers',{})))" 2>/dev/null || echo 'Traefik dashboard na porta 8080'
```
