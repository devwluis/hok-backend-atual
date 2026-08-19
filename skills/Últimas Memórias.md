# Últimas Memórias

## Ação
```bash
sqlite3 /root/hokma/backend/memory.db "SELECT role, substr(content,1,80), ts FROM memory ORDER BY ts DESC LIMIT 5;"
```
