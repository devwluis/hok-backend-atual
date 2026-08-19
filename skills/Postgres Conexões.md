# Postgres Conexões

## Ação

```bash
docker exec $(docker ps -q --filter name=postgres) psql -U postgres -c 'SELECT count(*) as conexoes FROM pg_stat_activity;' 2>/dev/null
```
