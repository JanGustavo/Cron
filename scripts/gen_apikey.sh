#!/bin/bash
# Gera uma API Key de exemplo para testes locais.
# Em produção, isso é feito pela API via /v1/auth/keys
KEY="cf_live_$(openssl rand -hex 32)"
HASH=$(echo -n "$KEY" | sha256sum | awk '{print $1}')
echo "API Key (guarde, não é salva no banco): $KEY"
echo "Hash SHA-256 (salvo no banco):          $HASH"
