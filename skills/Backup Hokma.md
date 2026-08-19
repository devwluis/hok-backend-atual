# Backup Hokma

## Ação

```bash
tar -czf /tmp/hokma-backup-$(date +%Y%m%d).tar.gz /root/hokma 2>/dev/null && echo '✅ Backup criado em /tmp'
```
