# Limpar Containers Exited

## Ação

```bash
docker rm $(docker ps -aq --filter status=exited) 2>/dev/null && echo '✅ Containers limpos' || echo 'Nada para limpar'
```
