# Redis Status

## Ação

```bash
docker exec $(docker ps -q --filter name=redis) redis-cli ping 2>/dev/null && docker exec $(docker ps -q --filter name=redis) redis-cli info server 2>/dev/null | grep -E 'version|uptime|connected'
```
