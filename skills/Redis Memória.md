# Redis Memória

## Ação

```bash
docker exec $(docker ps -q --filter name=redis) redis-cli info memory 2>/dev/null | grep -E 'used_memory_human|maxmemory_human'
```
