# Traefik Reiniciar

## Ação

```bash
docker service update --force traefik_traefik 2>&1 && echo '✅ Traefik reiniciado'
```
