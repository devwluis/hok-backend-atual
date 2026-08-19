# Redis Flush Cache

## Ação

```bash
docker exec $(docker ps -q --filter name=redis) redis-cli FLUSHDB 2>/dev/null && echo '✅ Cache Redis limpo'
```
