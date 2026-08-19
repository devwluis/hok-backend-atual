# system-health

## Descrição
Verifica saúde geral do sistema HOK OS

## Uso

```bash
echo "=== Processos ===" && ps aux | grep -E "hokma|cloudflared|node" | grep -v grep
echo "=== Portas ===" && ss -tlnp | grep -E "8082|8083"
echo "=== Disco ===" && df -h | grep -E "/$|home"
echo "=== RAM ===" && free -h
echo "=== Backend ===" && curl -s http://127.0.0.1:8082/health 2>/dev/null || echo "backend offline"
```
