#!/bin/bash
# ==============================================================================
# 🗄️ CronFlow PostgreSQL Automated Backup & Pruning Script
# ==============================================================================
# Executa dump completo do banco PostgreSQL, compacta com gzip e rotaciona (7 dias)
# ==============================================================================

set -euo pipefail

BACKUP_DIR="/opt/cronflow/backups/db"
LOG_FILE="/var/log/cronflow_backup.log"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILE="${BACKUP_DIR}/cronflow_db_${TIMESTAMP}.sql.gz"

mkdir -p "$BACKUP_DIR"
touch "$LOG_FILE" 2>/dev/null || LOG_FILE="/tmp/cronflow_backup.log"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

log "=== 🚀 Iniciando Backup do Banco PostgreSQL CronFlow ==="

# Se o .env existir, carrega DATABASE_URL
if [ -f /opt/cronflow/.env ]; then
    DATABASE_URL=$(grep "^DATABASE_URL=" /opt/cronflow/.env | cut -d '=' -f2- | tr -d '"' | tr -d "'")
else
    DATABASE_URL="${DATABASE_URL:-postgresql://cronflow:cronflow_secret@localhost:5432/cronflow?sslmode=disable}"
fi

# Executa pg_dump
if pg_dump "$DATABASE_URL" | gzip -9 > "$BACKUP_FILE"; then
    chmod 600 "$BACKUP_FILE"
    FILE_SIZE=$(du -h "$BACKUP_FILE" | cut -f1)
    log "✅ Backup gerado com sucesso: $BACKUP_FILE (Tamanho: $FILE_SIZE)"
else
    log "❌ Erro crítico ao executar pg_dump!"
    exit 1
fi

# Remove backups com mais de 7 dias
log "🧹 Verificando backups com mais de 7 dias para expurgo..."
DELETED_COUNT=$(find "$BACKUP_DIR" -type f -name "cronflow_db_*.sql.gz" -mtime +7 -print -delete 2>/dev/null | wc -l || echo 0)
log "✓ $DELETED_COUNT backup(s) antigo(s) removido(s)."

log "=== 🎉 Backup concluído com sucesso ==="
