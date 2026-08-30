#!/bin/bash
# ==============================================================================
# CronFlow Deployment Automation Script v2
# ==============================================================================
# Uso: ./deploy.sh [IP_DROPLET] [SSH_KEY_PATH]
# Ex:  ./deploy.sh 167.99.237.151 ~/.ssh/id_rsa
# ==============================================================================

set -euo pipefail

# CONFIGURAÇÕES
DROPLET_IP="${1:-64.236.155.209}"
DROPLET_USER="${3:-azureuser}"
SSH_KEY="${2:-/home/jandersongustavo/Músicas/vm-docker_key.pem}"
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ControlMaster=auto -o ControlPath=/tmp/ssh-%r@%h:%p -o ControlPersist=60s"
[[ -n "$SSH_KEY" ]] && SSH_OPTS="$SSH_OPTS -i $SSH_KEY"

# Cria diretório de logs se não existir
mkdir -p LogsDeploy

LOG_FILE="$(pwd)/LogsDeploy/deploy_$(date +%Y%m%d_%H%M%S).log"

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[$(date '+%H:%M:%S')]${NC} $*" | tee -a "$LOG_FILE"; }
warn() { echo -e "${YELLOW}[$(date '+%H:%M:%S')] ⚠️  $*${NC}" | tee -a "$LOG_FILE"; }
error() { echo -e "${RED}[$(date '+%H:%M:%S')] ❌ $*${NC}" | tee -a "$LOG_FILE"; }

# Função SSH com retry
ssh_exec() {
    ssh $SSH_OPTS "$DROPLET_USER@$DROPLET_IP" "$@"
}

scp_exec() {
    scp $SSH_OPTS "$@"
}

# Verifica conectividade
check_ssh() {
    log "Verificando conexão SSH com $DROPLET_IP..."
    if ! ssh_exec "echo 'OK'" >/dev/null 2>&1; then
        error "Falha na conexão SSH. Verifique IP, chave e firewall."
        exit 1
    fi
    log "SSH OK"
}

# ==============================================================================
# 1. BUILD FRONTEND
# ==============================================================================
build_frontend() {
    log "=== 1/7 Compilando Frontend React ==="
    cd "cron front"
    # Carrega nvm e usa a versão padrão
    export NVM_DIR="$HOME/.nvm"
    if [ -s "$NVM_DIR/nvm.sh" ]; then
        . "$NVM_DIR/nvm.sh"
        nvm use default >> "$LOG_FILE" 2>&1 || true
    fi
    if ! npm run build >> "$LOG_FILE" 2>&1; then
        error "Build do frontend falhou. Verifique $LOG_FILE"
        exit 1
    fi
    cd ..
    log "Frontend compilado em cron front/dist/"
}

# ==============================================================================
# 2. BUILD GO BINÁRIOS
# ==============================================================================
build_go() {
    log "=== 2/7 Compilando Binários Go (linux/amd64) ==="
    cd "cronflow"
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "v1.0.0-dev")
    BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    LDFLAGS="-w -s -X github.com/JanGustavo/Cron/internal/api/handler.Version=${VERSION} -X github.com/JanGustavo/Cron/internal/api/handler.BuildTime=${BUILD_TIME}"

    for bin in api scheduler worker; do
        log "  Compilando $bin..."
        GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "bin/$bin" "./cmd/$bin"
    done

    # Verifica se binários existem e são executáveis
    for bin in api scheduler worker; do
        [[ -f "bin/$bin" && -x "bin/$bin" ]] || { error "Binário $bin não gerado"; exit 1; }
    done
    cd ..
    log "Binários OK: api, scheduler, worker"
}

# ==============================================================================
# 3. PREPARA DIRETÓRIOS REMOTOS
# ==============================================================================
prepare_dirs() {
    log "=== 3/7 Preparando diretórios no Droplet ==="
    ssh_exec "mkdir -p /var/www/cron-front /opt/cronflow/bin /opt/cronflow/backups"
}

# ==============================================================================
# 4. DEPLOY FRONTEND
# ==============================================================================
deploy_frontend() {
    log "=== 4/7 Enviando Frontend via rsync ==="
    rsync -avz --delete -e "ssh $SSH_OPTS" "cron front/dist/" "$DROPLET_USER@$DROPLET_IP:/tmp/dist_cronfront"
    ssh_exec "sudo mkdir -p /var/www/cron-front && sudo cp -r /tmp/dist_cronfront/* /var/www/cron-front/ && sudo chown -R www-data:www-data /var/www/cron-front && sudo chmod -R 755 /var/www/cron-front"
}

# ==============================================================================
# 5. BACKUP E DEPLOY BINÁRIOS + CONFIGS
# ==============================================================================
deploy_backend() {
    log "=== 5/7 Parando serviços e fazendo backup ==="
    
    # Backup dos binários atuais
    ssh_exec "
        sudo mkdir -p /opt/cronflow/backups/\$(date +%Y%m%d_%H%M%S)
        sudo cp /opt/cronflow/bin/api /opt/cronflow/backups/\$(date +%Y%m%d_%H%M%S)/ 2>/dev/null || true
        sudo cp /opt/cronflow/bin/scheduler /opt/cronflow/backups/\$(date +%Y%m%d_%H%M%S)/ 2>/dev/null || true
        sudo cp /opt/cronflow/bin/worker /opt/cronflow/backups/\$(date +%Y%m%d_%H%M%S)/ 2>/dev/null || true
    "

    # Para serviços com grace period
    ssh_exec "sudo systemctl stop cronflow-api cronflow-scheduler cronflow-worker || true"
    sleep 2

    log "Enviando binários..."
    scp_exec cronflow/bin/api "$DROPLET_USER@$DROPLET_IP:/tmp/cronflow_api"
    scp_exec cronflow/bin/scheduler "$DROPLET_USER@$DROPLET_IP:/tmp/cronflow_scheduler"
    scp_exec cronflow/bin/worker "$DROPLET_USER@$DROPLET_IP:/tmp/cronflow_worker"

    ssh_exec "
        sudo mkdir -p /opt/cronflow/bin
        sudo mv /tmp/cronflow_api /opt/cronflow/bin/api
        sudo mv /tmp/cronflow_scheduler /opt/cronflow/bin/scheduler
        sudo mv /tmp/cronflow_worker /opt/cronflow/bin/worker
        sudo chmod +x /opt/cronflow/bin/*
    "

    log "Enviando systemd services..."
    scp_exec deploy/cronflow-api.service "$DROPLET_USER@$DROPLET_IP:/tmp/cronflow-api.service"
    scp_exec deploy/cronflow-scheduler.service "$DROPLET_USER@$DROPLET_IP:/tmp/cronflow-scheduler.service"
    scp_exec deploy/cronflow-worker.service "$DROPLET_USER@$DROPLET_IP:/tmp/cronflow-worker.service"
    ssh_exec "sudo mv /tmp/cronflow*.service /etc/systemd/system/"

    log "Configurando rotina de backup diário do PostgreSQL..."
    scp_exec scripts/backup_db.sh "$DROPLET_USER@$DROPLET_IP:/tmp/backup_db.sh"
    ssh_exec "
        sudo mkdir -p /opt/cronflow/scripts /opt/cronflow/backups/db
        sudo mv /tmp/backup_db.sh /opt/cronflow/scripts/backup_db.sh
        sudo chmod +x /opt/cronflow/scripts/backup_db.sh
        (sudo crontab -l 2>/dev/null | grep -v 'backup_db.sh' || true; echo '0 3 * * * /opt/cronflow/scripts/backup_db.sh >/dev/null 2>&1') | sudo crontab -
    "
}

# ==============================================================================
# 6. SYNC .ENV (apenas chaves novas, preserva produção)
# ==============================================================================
sync_env() {
    log "=== 6/7 Sincronizando .env (apenas chaves novas) ==="

    if ! ssh_exec "[ -f /opt/cronflow/.env ]"; then
        log "Primeiro deploy - enviando .env completo"
        scp_exec cronflow/.env "$DROPLET_USER@$DROPLET_IP:/opt/cronflow/.env"
    else
        log "Adicionando chaves ausentes e atualizando faturamento..."
        
        cat << 'EOF' > /tmp/cronflow_sync_env.sh
#!/bin/bash
KEYS=(
    APP_ENV PORT DATABASE_URL REDIS_URL
    SCHEDULER_LOCK_TTL SCHEDULER_INTERVAL
    WORKER_CONCURRENCY WORKER_TIMEOUT_DEFAULT
    MAX_JOBS_FREE_PLAN MAX_JOBS_PAID_PLAN
    LOG_RETENTION_FREE_DAYS LOG_RETENTION_PAID_DAYS
    JWT_SECRET
    GEMINI_API_KEY GROQ_API_KEY DISABLE_GEMINI
    SMTP_HOST SMTP_PORT SMTP_USER SMTP_PASS SMTP_FROM RESEND_API_KEY
    GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET
    GITHUB_CLIENT_ID GITHUB_CLIENT_SECRET
    API_URL FRONTEND_URL
    BILLING_PROVIDER ASAAS_API_KEY ASAAS_WEBHOOK_TOKEN
)
for key in "${KEYS[@]}"; do
    local_line=$(grep "^${key}=" /tmp/local.env 2>/dev/null || true)
    if [[ -n "$local_line" ]]; then
        if ! grep -q "^${key}=" /opt/cronflow/.env; then
            echo "" >> /opt/cronflow/.env
            echo "$local_line" >> /opt/cronflow/.env
        elif [[ "$key" == "BILLING_PROVIDER" || "$key" == "ASAAS_API_KEY" || "$key" == "ASAAS_WEBHOOK_TOKEN" ]]; then
            awk -v k="$key" -v val="$local_line" '$0 ~ "^"k"=" {$0=val} {print}' /opt/cronflow/.env > /tmp/env.tmp && mv /tmp/env.tmp /opt/cronflow/.env
        fi
    fi
done
EOF

        scp_exec cronflow/.env "$DROPLET_USER@$DROPLET_IP:/tmp/local.env"
        scp_exec /tmp/cronflow_sync_env.sh "$DROPLET_USER@$DROPLET_IP:/tmp/sync_env.sh"
        
        ssh_exec "sudo bash /tmp/sync_env.sh && sudo chmod 600 /opt/cronflow/.env && rm -f /tmp/local.env /tmp/sync_env.sh"
        rm -f /tmp/cronflow_sync_env.sh
    fi
}

# ==============================================================================
# 7. NGINX + HEALTH CHECK + RESTART
# ==============================================================================
deploy_nginx_and_restart() {
    log "=== 7/7 Configurando Nginx e reiniciando serviços ==="

    # Nginx (apenas primeira vez)
    if ! ssh_exec "[ -f /etc/nginx/sites-available/cronflow ]"; then
        log "Primeiro deploy - enviando config Nginx"
        scp_exec deploy/cronflow "$DROPLET_USER@$DROPLET_IP:/etc/nginx/sites-available/cronflow"
        ssh_exec "ln -sf /etc/nginx/sites-available/cronflow /etc/nginx/sites-enabled/"
        if ! ssh_exec "sudo nginx -t"; then
            error "nginx -t falhou! Verifique config."
            exit 1
        fi
        ssh_exec "sudo systemctl reload nginx"
    else
        log "Nginx já configurado (SSL/Certbot preservado)"
    fi

    # Systemd reload + restart
    ssh_exec "sudo systemctl daemon-reload"
    ssh_exec "sudo systemctl enable cronflow-api cronflow-scheduler cronflow-worker"
    ssh_exec "sudo systemctl restart cronflow-api cronflow-scheduler cronflow-worker"

    # Health check com retry
    log "Aguardando serviços subirem..."
    sleep 3

    local max_attempts=10
    local attempt=1
    while [[ $attempt -le $max_attempts ]]; do
        if ssh_exec "systemctl is-active --quiet cronflow-api && systemctl is-active --quiet cronflow-scheduler && systemctl is-active --quiet cronflow-worker"; then
            log "✅ Todos os serviços ativos"
            
            # HTTP health check
            if ssh_exec "curl -sf http://localhost:8080/health >/dev/null"; then
                log "✅ Health check HTTP OK"
            else
                warn "Health check HTTP falhou (pode ser normal se não exposto localmente)"
            fi

            # AI Models health check
            log "Verificando disponibilidade dos modelos de IA no Droplet..."
            local ai_check
            ai_check=$(ssh_exec "curl -s http://localhost:8080/v1/health/ai" || true)
            if [[ -n "$ai_check" ]]; then
                log "Resultado da verificação de IA: $ai_check"
                if echo "$ai_check" | grep -q '"status":"ok"'; then
                    log "✅ Modelos de IA operacionais e validados!"
                else
                    error "❌ Falha crítica: Modelos de IA indisponíveis ou inválidos!"
                    exit 1
                fi
            else
                warn "Não foi possível obter resposta do endpoint de IA."
            fi

            return 0
        fi
        log "  Tentativa $attempt/$max_attempts - aguardando..."
        sleep 2
        ((attempt++))
    done

    error "Serviços não subiram após $max_attempts tentativas"
    log "Status dos serviços:"
    ssh_exec "systemctl status cronflow-api cronflow-scheduler cronflow-worker --no-pager"
    log "Logs recentes:"
    ssh_exec "journalctl -u cronflow-api -u cronflow-scheduler -u cronflow-worker --no-pager -n 50"
    exit 1
}

# ==============================================================================
# MAIN
# ==============================================================================
main() {
    log "=== 🚀 CronFlow Deploy Iniciado ==="
    log "Target: $DROPLET_USER@$DROPLET_IP"
    log "Log: $LOG_FILE"

    check_ssh
    build_frontend
    build_go
    prepare_dirs
    deploy_frontend
    deploy_backend
    sync_env
    deploy_nginx_and_restart

    log "=== 🎉 DEPLOY CONCLUÍDO COM SUCESSO! ==="
    log "Frontend: https://cronflow.jangustavo.me"
    log "API:      https://cronflow.jangustavo.me/api"
    log "Log salvo em: $LOG_FILE"
}

main "$@"