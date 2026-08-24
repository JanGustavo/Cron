# Relatório de Latência e Estresse de Conexões — CronFlow Produção

*   **Host da API:** `https://cronflow.jangustavo.me`
*   **Usuário de Teste:** `jeeh2200@gmail.com`
*   **Volume Total de Disparos Concorrentes:** `90 execuções` (2 jobs × 45 disparos/cada)

## 📊 1. Estatísticas de Latência por Categoria
| Categoria | Endpoint Alvo | Amostras | Min Latência | Média Latência | Mediana Latência | Max Latência |
|---|---|---|---|---|---|---|
| LOCAL | `https://cronflow.jangustavo.me/health` | 45 | 1ms | 11.73ms | 2ms | 181ms |
| DISTANT_US | `https://httpbin.org/get` | 45 | 8ms | 51.13ms | 11ms | 710ms |

## 🧠 2. Uso de Memória e Goroutines (Go Runtime)
| Métrica | Baseline (Repouso) | Pico (Durante Carga) | Cooldown (Pós-Carga) |
|---|---|---|---|
| Goroutines Ativas | 5 | 18 | 5 |
| Memória Alocada (Heap) | 1.04 MB | 2.88 MB | 0.94 MB |
| Memória Reservada (Sys) | 7.96 MB | 12.21 MB | 12.77 MB |
| Heap Reservado (Sys) | 3.72 MB | 7.53 MB | 7.62 MB |
| Objetos no Heap | 6996 | 29298 | 4442 |
| Contagem GC (Geral) | 0 | 0 | 1 |