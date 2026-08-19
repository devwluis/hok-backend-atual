# Restaurar Backup

## O que faz
Restaura o backend para o ultimo commit estavel no Git quando ha erro critico.

## Acao

```bash
cd /root/hokma/backend
echo "=== Estado atual ==="
git status --short
echo "=== Revertendo para ultimo backup ==="
git stash
git reset --hard HEAD
go build -o hokma . && echo "BUILD OK" || echo "BUILD FALHOU"
echo "=== Reiniciando servico ==="
systemctl restart hokma
sleep 2
curl -s http://127.0.0.1:8082/health | python3 -c "import sys,json; d=json.load(sys.stdin); print('Online:', d['status'])"
```

## Prompts que ativam esta skill
- "restaurar backup"
- "reverter codigo"
- "sistema comprometido restaurar"
- "rollback do backend"
- "desfazer ultima alteracao"
