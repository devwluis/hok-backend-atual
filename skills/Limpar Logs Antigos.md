# Limpar Logs Antigos

## Ação

```bash
sqlite3 /root/hokma/backend/memory.db 'DELETE FROM logs WHERE id < (SELECT MAX(id)-1000 FROM logs);' && echo 'Logs antigos removidos'
```
