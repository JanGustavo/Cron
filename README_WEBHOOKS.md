# 🚨 Documentação de Webhooks e Assinaturas HMAC-SHA256 do CronFlow

O CronFlow envia notificações de webhook quando suas tarefas falham consecutivamente. Para garantir que as requisições recebidas pelo seu servidor de alertas de fato foram enviadas pelo CronFlow e não sofreram alterações em trânsito, implementamos um sistema de assinaturas criptográficas baseado em **HMAC-SHA256**.

---

## 📬 Estrutura da Requisição de Alerta

O CronFlow enviará uma chamada HTTP `POST` para a URL do seu webhook configurado.

### Cabeçalhos HTTP Enviados
*   `Content-Type`: `application/json`
*   `User-Agent`: `CronFlow-Alerter/1.0`
*   `X-CronFlow-Timestamp`: Carimbo de data/hora Unix (em segundos) do momento em que o alerta foi gerado.
*   `X-CronFlow-Signature`: Contém o timestamp e a assinatura no formato `t=<timestamp>,v1=<signature>`.

### Exemplo de Payload JSON (Event: `job.failing`)
```json
{
  "job_id": "b39bf388-056e-416f-8a11-e30f0c6ab637",
  "job_name": "Sincronização de Vendas",
  "event": "job.failing",
  "consecutive_failures": 3,
  "last_http_status": 500,
  "last_response_body": "Internal Server Error",
  "triggered_at": "2026-08-10T14:34:10Z"
}
```

---

## 🔒 Como a Assinatura é Calculada

1.  **Segredo do Webhook (Chave):**
    A chave secreta (`whsec_...`) está disponível para visualização no painel de perfil do seu projeto no CronFlow. Ela é gerada de forma segura e exclusiva para o seu workspace.
2.  **String para Assinatura:**
    A string a ser assinada é a concatenação do timestamp do cabeçalho `X-CronFlow-Timestamp`, um caractere ponto `.`, e o corpo JSON bruto da requisição HTTP (em bytes).
    ```text
    <timestamp>.<payload_bruto>
    ```
3.  **Algoritmo:**
    Calcula-se o hash HMAC usando **SHA-256** com a string de assinatura criada acima e a chave secreta. O resultado é codificado em formato hexadecimal.

---

## 🛠️ Exemplos Práticos de Validação

Para se proteger contra **ataques de temporização (Timing Attacks)**, use sempre funções de comparação em tempo constante (`compare_digest`, `timingSafeEqual`, etc.). Para se proteger contra **ataques de replay**, verifique se a diferença entre o timestamp do cabeçalho e a hora do seu servidor está dentro de um limite seguro (ex: 5 minutos).

### 1. 🐍 Python (Flask/FastAPI)

```python
import hmac
import hashlib
import time

def verify_webhook(raw_payload: bytes, header_signature: str, webhook_secret: str, max_age_seconds: int = 300) -> bool:
    """
    Verifica a autenticidade e integridade do webhook recebido do CronFlow.
    
    :param raw_payload: Corpo bruto do request (bytes).
    :param header_signature: Valor do cabeçalho 'X-CronFlow-Signature'.
    :param webhook_secret: O segredo whsec_... obtido no painel do CronFlow.
    :param max_age_seconds: Tempo máximo aceitável para mitigar replay attacks.
    """
    try:
        # 1. Parse do cabeçalho: t=1234567,v1=abcdef...
        parts = dict(item.split("=") for item in header_signature.split(","))
        timestamp = parts.get("t")
        signature = parts.get("v1")
    except Exception:
        return False
    
    if not timestamp or not signature:
        return False
        
    # 2. Mitigar Replay Attacks
    try:
        ts = int(timestamp)
    except ValueError:
        return False
        
    if abs(time.time() - ts) > max_age_seconds:
        return False # Requisição expirada
        
    # 3. Recriar a string de assinatura: <timestamp>.<payload>
    signing_string = f"{timestamp}.".encode("utf-8") + raw_payload
    
    # 4. Calcular o HMAC esperado
    expected_signature = hmac.new(
        webhook_secret.encode("utf-8"),
        signing_string,
        hashlib.sha256
    ).hexdigest()
    
    # 5. Comparação em tempo constante contra timing attacks
    return hmac.compare_digest(expected_signature, signature)
```

---

### 2. 🟢 Node.js (Express/Koa)

> **Importante:** Você deve capturar o corpo da requisição como `Buffer` ou string crua (`rawBody`). Bibliotecas como `body-parser.json()` alteram a string original do payload, invalidando a assinatura.

```javascript
const crypto = require('crypto');

/**
 * Verifica a assinatura do webhook do CronFlow.
 * 
 * @param {string|Buffer} rawBody - Corpo bruto (não parseado) da requisição.
 * @param {string} headerSignature - Cabeçalho 'X-CronFlow-Signature'.
 * @param {string} webhookSecret - Segredo do webhook (whsec_...).
 * @param {number} maxAgeSeconds - Tolerância contra replay attacks (padrão: 300s).
 */
function verifyWebhook(rawBody, headerSignature, webhookSecret, maxAgeSeconds = 300) {
  try {
    // 1. Parse do cabeçalho: t=1234567,v1=abcdef...
    const parts = {};
    headerSignature.split(',').forEach(part => {
      const [key, value] = part.split('=');
      parts[key] = value;
    });
    
    const timestamp = parts['t'];
    const signature = parts['v1'];
    
    if (!timestamp || !signature) {
      return false;
    }
    
    // 2. Mitigar Replay Attacks
    const ts = parseInt(timestamp, 10);
    if (isNaN(ts)) {
      return false;
    }
    
    const now = Math.floor(Date.now() / 1000);
    if (Math.abs(now - ts) > maxAgeSeconds) {
      return false;
    }
    
    // 3. Recriar string de assinatura
    const signingString = `${timestamp}.${rawBody}`;
    
    // 4. Calcular o HMAC esperado
    const expectedSignature = crypto
      .createHmac('sha256', webhookSecret)
      .update(signingString)
      .digest('hex');
      
    // 5. Comparação segura em tempo constante
    const signatureBuffer = Buffer.from(signature, 'hex');
    const expectedBuffer = Buffer.from(expectedSignature, 'hex');
    
    if (signatureBuffer.length !== expectedBuffer.length) {
      return false;
    }
    
    return crypto.timingSafeEqual(signatureBuffer, expectedBuffer);
  } catch (err) {
    return false;
  }
}
```

---

### 3. 🔵 Go (Golang)

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// VerifyWebhook valida a assinatura do webhook do CronFlow em Go.
func VerifyWebhook(payload []byte, headerSignature string, webhookSecret string, maxAge time.Duration) error {
	// 1. Parse do cabeçalho: t=1234567,v1=abcdef...
	var timestamp string
	var signature string
	
	parts := strings.Split(headerSignature, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			if kv[0] == "t" {
				timestamp = kv[1]
			} else if kv[0] == "v1" {
				signature = kv[1]
			}
		}
	}
	
	if timestamp == "" || signature == "" {
		return errors.New("cabeçalhos de assinatura ou data ausentes")
	}
	
	// 2. Mitigar Replay Attacks
	var ts int64
	if _, err := fmt.Sscanf(timestamp, "%d", &ts); err != nil {
		return errors.New("formato de timestamp inválido")
	}
	
	sentTime := time.Unix(ts, 0)
	if math.Abs(time.Since(sentTime).Seconds()) > maxAge.Seconds() {
		return errors.New("assinatura expirada (possível replay attack)")
	}
	
	// 3. Recriar string de assinatura: <timestamp>.<payload>
	signingString := fmt.Sprintf("%s.%s", timestamp, string(payload))
	
	// 4. Calcular HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(signingString))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	
	// 5. Comparação em tempo constante
	if subtle.ConstantTimeCompare([]byte(expectedSignature), []byte(signature)) != 1 {
		return errors.New("assinatura inválida")
	}
	
	return nil
}
```
