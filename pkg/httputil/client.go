package httputil

// HTTPClient — wrapper sobre net/http para execução dos jobs.
//   - Timeout configurável por job
//   - Executa GET ou POST com headers e payload
//   - Retorna status code, body truncado em 2KB e duração em ms
//   - Status >= 400 NÃO é tratado como error aqui — decisão do Worker
// Client reutilizado (connection pooling automático do Go).
