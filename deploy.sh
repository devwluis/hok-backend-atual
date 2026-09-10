#!/bin/bash
# deploy.sh — Build + test + deploy del backend HOK OS
# Uso: ./deploy.sh
# Detiene en cualquier etapa que falle (set -e).

set -euo pipefail

cd /root/hokma/backend

echo ""
echo "=============================================="
echo "  HOK OS — Deploy backend"
echo "=============================================="

# 1. Build aislado (NO toca el binario en producción)
echo ""
echo "▶ [1/8] go build -o hokma_new ."
go build -o hokma_new .
NEW_HASH=$(md5sum hokma_new | awk '{print $1}')
echo "✅ Build OK — hash: $NEW_HASH"

# 2. Tests — si fallan, abortar sin tocar producción
echo ""
echo "▶ [2/8] go test ./..."
go test ./...
echo "✅ Tests OK"

# 3. Detener el servicio
echo ""
echo "▶ [3/8] systemctl stop hokma"
systemctl stop hokma
echo "✅ Servicio detenido"

# 4. Backup del binario actual
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP="hokma.bak_${TIMESTAMP}"
echo ""
echo "▶ [4/8] Backup: cp hokma ${BACKUP}"
cp hokma "$BACKUP"
echo "✅ Backup creado: ${BACKUP}"

# 5. Reemplazar binario (mv preserva owner/permissions del nuevo build)
echo ""
echo "▶ [5/8] mv hokma_new hokma"
mv hokma_new hokma
echo "✅ Binario reemplazado"

# 6. Iniciar el servicio
echo ""
echo "▶ [6/8] systemctl start hokma"
systemctl start hokma
echo "✅ Servicio iniciado"

# 7. Estado del servicio
echo ""
echo "▶ [7/8] systemctl status hokma --no-pager"
systemctl status hokma --no-pager

# 7.5 Health check — si la app no responde, abortar antes de comparar hashes.
# El backend tarda unos segundos en abrir la porta tras el arranque: reintentar
# hasta 15s antes de considerarlo un fallo (evita falso negativo por timing).
echo ""
echo "▶ [7.5/8] Health check"
HEALTH="000"
for _i in $(seq 1 15); do
    HEALTH=$(curl -s -o /dev/null -w "%{http_code}" localhost:8082/health || echo "000")
    [ "$HEALTH" = "200" ] && break
    sleep 1
done
if [ "$HEALTH" != "200" ]; then
    echo "❌ Health check falhou (HTTP $HEALTH)"
    exit 1
fi
echo "✅ Health check OK (HTTP $HEALTH)"

# 8. Verificar hash — comparar binario guardado (paso 1) vs el que está rodando
RUNNING_HASH=$(md5sum hokma | awk '{print $1}')
echo ""
echo "▶ [8/8] Verificación de hashes"
echo "  Build generado: $NEW_HASH"
echo "  Rodando:        $RUNNING_HASH"
if [ "$NEW_HASH" = "$RUNNING_HASH" ]; then
    echo "✅ Hashes idénticos — deploy correcto"
else
    echo "❌ ERROR: los hashes NO coinciden"
    exit 1
fi

echo ""
echo "=============================================="
echo "  Deploy completado ✓"
echo "=============================================="