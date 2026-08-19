# Postgres Status

## Ação

```bash
docker exec $(docker ps -q --filter name=postgres) psql -U postgres -c '\l' 2>/dev/null || echo 'Postgres: verificar container'
```
