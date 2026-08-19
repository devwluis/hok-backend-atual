# Cron Jobs Hokma

## Ação

```bash
crontab -l 2>/dev/null | grep -v '#' || echo 'sem crons'
```
