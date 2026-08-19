# Ver Redes Docker

## Ação

```bash
docker network ls && docker network inspect $(docker network ls -q) 2>/dev/null | python3 -c "import json,sys; nets=json.load(sys.stdin); [print(n['Name'],'-',len(n.get('Containers',{})),'containers') for n in nets]"
```
