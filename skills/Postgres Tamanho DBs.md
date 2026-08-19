# Postgres Tamanho DBs

## Ação

```bash
docker exec $(docker ps -q --filter name=postgres) psql -U postgres -c 'SELECT datname, pg_size_pretty(pg_database_size(datname)) FROM pg_database;' 2>/dev/null
```
