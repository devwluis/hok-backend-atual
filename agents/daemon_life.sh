#!/bin/bash
# daemon_life.sh - O Daemon da Vida (Hokmá v14)
# Mantém o celular acordado, roda o heartbeat periodicamente,
# fala em voz alta em alertas críticos e aciona a consolidação às 23h.

# Garante que o Termux não durma quando a tela apagar
termux-wake-lock

DB_PATH="/data/data/com.termux/files/home/ecossistema/backend/memory.db"
CONSOLIDADO_HOJE=0
LAST_DISK_ALERT=0
LAST_BATT_ALERT=0

echo "=================================================="
echo " 🧬 Daemon da Vida Iniciado (Autonomia Ativa)..."
echo "=================================================="

while true; do
    # 1. Executa o sincronizador corpo-mente
    python3 /data/data/com.termux/files/home/ecossistema/backend/agents/agent_heartbeat.py > /dev/null 2>&1

    # 2. Ler o último token do subconsciente (forçando texto puro e ignorando o .sqliterc)
    TOKEN=$(sqlite3 -batch -init /dev/null $DB_PATH "SELECT token_string FROM neural_states ORDER BY id DESC LIMIT 1;" 2>/dev/null || true)

    # Extrair bateria e humor do token usando regex simples do bash
    if [[ $TOKEN =~ BATT:([0-9]+) ]] && [[ $TOKEN =~ MOOD:([0-9]+) ]]; then
        # Extração segura e isolada usando Regex nativo do Bash (imune a quebras)
        [[ $TOKEN =~ BATT:([0-9]+) ]] && BATT_VAL="${BASH_REMATCH[1]}" || BATT_VAL="0"
        [[ $TOKEN =~ DISK:([0-9]+) ]] && DISK_VAL="${BASH_REMATCH[1]}" || DISK_VAL="0"
        [[ $TOKEN =~ MOOD:([0-9]+) ]] && MOOD_VAL="${BASH_REMATCH[1]}" || MOOD_VAL="0"

        # Conversão numérica decimal segura (evita octal com zeros à esquerda)
        BATT_VAL_INT=$((10#$BATT_VAL))
        DISK_VAL_INT=$((10#$DISK_VAL))
        MOOD_VAL_INT=$((10#$MOOD_VAL))

        # Mantém a compatibilidade com o restante do script
        BATT_LEVEL=$BATT_VAL_INT
        MOOD_LEVEL=$MOOD_VAL_INT
        
        MOOD_VAL=$(echo $TOKEN | cut -d':' -f4) # "001"
        
        # 3. Análise física proativa (Gatilhos de voz e alertas físicos)
        # Alerta de bateria crítica física
        if [ $BATT_VAL_INT -lt 15 ]; then
            # Verifica se o carregador está desconectado
            CHARGING=$(termux-battery-status | grep -i "status" | grep -i -E "charging|full" || true)
            if [ -z "$CHARGING" ]; then
                AHORA=$(date +%s)
                # Cooldown de 15 minutos (900 segundos) para bateria
                if [ $((AHORA - LAST_BATT_ALERT)) -ge 900 ]; then
                    termux-tts-speak "Atenção Washington, minha bateria está em $BATT_VAL_INT por cento. Por favor, conecte o carregador."
                    termux-toast -b red "⚠️ Bateria em $BATT_VAL_INT%"
                    LAST_BATT_ALERT=$AHORA
                fi
            fi
        fi

        # Alerta de disco cheio físico
        if [ $DISK_VAL_INT -gt 85 ]; then
            AHORA=$(date +%s)
            # Cooldown de 1 hora (3600 segundos) para armazenamento
            if [ $((AHORA - LAST_DISK_ALERT)) -ge 3600 ]; then
                termux-tts-speak "Aviso, meu espaço de armazenamento está crítico."
                termux-notification -t "🚨 Armazenamento Crítico" -c "Apenas $((100 - DISK_VAL_INT))% livre. Rodando auto-cura."
                LAST_DISK_ALERT=$AHORA
            fi
        fication -t "🚨 Armazenamento Crítico" -c "Apenas $((100 - DISK_VAL_INT))% livre. Rodando auto-cura."
        fi
    fi

    # 4. Checagem de Hora para o Ciclo de Consolidação Semântica (Sono das 23h)
    HORA_ATUAL=$(date +"%H")
    if [ "$HORA_ATUAL" -eq "23" ]; then
        if [ $CONSOLIDADO_HOJE -eq 0 ]; then
            echo "🌙 [SONO] Passou das 23:00. Iniciando a consolidação das memórias..."
            python3 /data/data/com.termux/files/home/ecossistema/backend/agents/consolidar_mente.py > /dev/null 2>&1 || true
            CONSOLIDADO_HOJE=1
        fi
    else
        # Reseta o gatilho fora da janela das 23h
        CONSOLIDADO_HOJE=0
LAST_DISK_ALERT=0
LAST_BATT_ALERT=0
    fi

    # Dorme por 10 minutos antes da próxima varredura sensorial
    sleep 600
done
