# Reiniciar Todos Serviços

## Ação

```bash
docker service ls --format '{{.Name}}' | xargs -I{} docker service update --force {} 2>&1 && echo '✅ Todos serviços reiniciados'
```
