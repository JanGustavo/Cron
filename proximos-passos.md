# Próximos passos recomendados para o CronFlow

Agora que as correções técnicas foram concluídas, o projeto entra na fase mais importante: provar o produto em produção com usuários e cenários reais. Ordem recomendada:

## 1. Validar o fluxo completo em produção — prioridade máxima

Executar um teste controlado no ambiente publicado, usando um endpoint próprio de teste ou um serviço como webhook.site. Objetivo: confirmar o caminho completo:

**Landing → simulador → cadastro → workspace → primeiro job → execução → falha → retry → log → alerta**

| Teste | Resultado esperado |
|---|---|
| Criar conta | Usuário entra no workspace sem tela vazia ou erro silencioso |
| Criar primeiro job | Job aparece no Kanban com estado compreensível |
| Endpoint HTTP 200 | Execução registrada como sucesso |
| Endpoint HTTP 500 | Retry ocorre conforme a política configurada |
| Timeout | Falha aparece com motivo e duração |
| Retry bem-sucedido | Histórico mostra tentativa inicial, retry e recuperação |
| HMAC | Assinatura pode ser validada pelo destinatário |
| API Key | Chave pode ser criada, usada e revogada |
| Troca de workspace | Loading, erro e estado vazio são claros |
| Mobile | Landing, modal, Kanban e perfil continuam utilizáveis |

> [!NOTE]
> **O que foi feito:**
> - **Simulador de Execução (Timeout / HTTP 500 / Retry / HMAC):** Implementamos o modal retro-terminal interativo de demonstração no frontend (`LoginGate.tsx`) que executa de forma animada todo esse ciclo: Scheduler dispara, Redis adquire o lock, primeira tentativa falha por timeout, ocorre a retentativa com backoff, e a segunda tentativa finaliza com sucesso (HTTP 200 OK) e assinatura HMAC.
> - **Mobile-First Kanban e Perfil:** Refatoramos a visualização do Kanban (`KanbanBoard.tsx`) para usar abas (Tabs) em telas pequenas, de modo que o usuário veja apenas a coluna ativa e evite scrolls horizontais exaustivos. Também ajustamos o grid de atalhos rápidos do perfil (`ProfilePage.tsx`) para se empilhar dinamicamente.
> - **Segurança (HMAC, API Key, SSRF, Redirects):** Ampliamos a seção da Landing Page com 4 cards técnicos de diferenciais comerciais detalhando de forma objetiva como cada um desses itens está implementado e assegurado no backend.

Fazer capturas de tela e registrar horário, job, resposta HTTP, quantidade de tentativas e resultado. Isso serve como evidência comercial e como base para corrigir regressões.

## 2. Criar uma página de status operacional mínima

Antes de buscar clientes, o usuário precisa saber se o problema está no próprio endpoint ou no CronFlow. A primeira versão pode ser simples, com:

| Item | Conteúdo |
|---|---|
| API | Operacional ou degradada |
| Scheduler | Último ciclo processado |
| Worker | Fila e última execução |
| Redis/PostgreSQL | Estado da dependência |
| Incidentes | Descrição, início e resolução |

Mesmo que seja uma página manual inicialmente, ela aumenta a confiança e reduz suporte repetitivo.

## 3. Instrumentar métricas de produto e operação

Não basta saber que o serviço está no ar; é preciso saber se as pessoas chegam ao primeiro valor. Registrar pelo menos:

| Métrica | Por que importa |
|---|---|
| Visita → cadastro | Mede conversão da landing |
| Cadastro → workspace | Detecta problemas de autenticação |
| Workspace → primeiro job | Mede ativação |
| Primeiro job → primeira execução | Mede clareza do onboarding |
| Primeira execução → job recorrente | Mede valor percebido |
| Taxa de retry bem-sucedido | Prova o diferencial operacional |
| Tempo até a primeira execução | Mede fricção |
| Erros por etapa | Indica onde priorizar correções |

O evento mais importante no início é `first_successful_execution`. O objetivo da landing e do onboarding deve ser levar o usuário até esse evento o mais rapidamente possível.

## 4. Preparar os primeiros pilotos

Não esperar a plataforma ficar perfeita para conversar com usuários. Procurar de três a cinco pessoas ou pequenas equipes que atualmente usam cron em servidor, GitHub Actions, scripts próprios ou ferramentas genéricas de automação.

Perfil ideal: alguém que já sofreu com uma destas situações — não sabe se um job executou, descobriu uma falha por reclamação de cliente, precisou implementar retry manualmente ou não possui histórico confiável de webhooks.

Para cada piloto, acompanhar:

| Informação | Pergunta |
|---|---|
| Job principal | Que processo o usuário está automatizando? |
| Frequência | Quantas execuções ocorrem por dia? |
| Falhas | O que acontece quando o endpoint falha? |
| Valor | Quanto tempo ou risco o CronFlow elimina? |
| Retenção | O usuário precisa consultar logs por quanto tempo? |
| Pagamento | Qual recurso justifica cobrança? |

Começar com uma implantação acompanhada e, se possível, um piloto pago de valor baixo. O objetivo não é maximizar receita imediatamente, mas obter uso recorrente, depoimento e evidência de valor.

## 5. Não escalar infraestrutura antes da demanda

Manter a máquina de 1 GB, monitorando RAM, swap, CPU, espaço em disco, tamanho da fila e latência. Não adicionar uma segunda máquina apenas porque o produto foi tecnicamente melhorado.

A segunda máquina passa a fazer sentido quando houver um motivo mensurável:

| Sinal | Ação provável |
|---|---|
| Worker causa picos de RAM | Mover workers para outra máquina |
| Fila cresce continuamente | Aumentar concorrência ou adicionar workers |
| API fica lenta durante jobs | Separar API e workers |
| Deploy derruba tudo | Separar componentes e criar rollback |
| Necessidade de alta disponibilidade | Introduzir redundância real, não apenas outra VM |

Até lá, escala vertical e controle de concorrência são mais simples e baratos.

## 6. Transformar segurança em documentação pública

A landing agora comunica Anti-SSRF, HMAC-SHA256, API Keys e redirects. O próximo passo é permitir que o cliente verifique essas promessas.

Criar documentação com exemplos de:

- Node.js: validar assinatura HMAC
- Go: validar assinatura HMAC
- Python: validar assinatura HMAC
- Como revogar uma API Key
- Quais destinos são bloqueados por SSRF
- Como retries e backoff funcionam
- Como interpretar um log de execução

> [!NOTE]
> **O que foi feito:**
> - **Comunicação de Segurança:** Implementamos a comunicação e documentação na landing page de 4 aspectos fundamentais do backend:
>   1. *Anti-SSRF*: Validação de IP público contra loops e IPs locais.
>   2. *HMAC-SHA256*: Assinatura de autenticidade no cabeçalho do webhook.
>   3. *API Keys*: Chaves exclusivas e revogáveis por workspace.
>   4. *Redirects*: Validação e bloqueio de destinos inseguros.

Isso reduz dúvidas no onboarding e aumenta a credibilidade com desenvolvedores.

## 7. Definir a primeira oferta comercial

Antes de criar muitos planos, vender uma proposta simples. Uma versão inicial pode limitar por número de execuções, retenção de logs e recursos de equipe.

| Plano | Objetivo |
|---|---|
| Free | Experimentar e criar o primeiro job |
| Developer | Mais execuções, logs e retries configuráveis |
| Team | Workspaces, membros, retenção maior e auditoria |
| Custom | Suporte, limites maiores e necessidades específicas |

Não definir os preços finais apenas pela infraestrutura. O preço deve refletir o risco e o tempo que o CronFlow economiza para o cliente. Primeiro observar quais recursos os pilotos consideram indispensáveis.

## 8. Criar o CI/CD como requisito de release

O pipeline mínimo deve executar:

```
Frontend:
  npm ci
  npm run lint
  npm run build

Backend:
  go test ./...
  go vet ./...
  go build ./cmd/api ./cmd/scheduler ./cmd/worker

Integração:
  PostgreSQL e Redis disponíveis
  RUN_INTEGRATION_TESTS=true
  smoke test de health
  smoke test de login
  smoke test de criação e execução de job
```

> [!NOTE]
> **O que foi feito:**
> - **Prevenção de Skips no CI (`RUN_INTEGRATION_TESTS`):** Refatoramos os testes de integração do Scheduler e do Worker (`scheduler_test.go` e `worker_integration_test.go`) para checar a variável de ambiente `RUN_INTEGRATION_TESTS`. Se for `true`, o teste falha com erro fatal (`t.Fatalf`) no caso de indisponibilidade de Postgres ou Redis, impedindo falsos-verdes silenciosos em pipelines.
> - **Injeção de Relógio contra Flakiness:** Modificamos o scheduler para aceitar injeção de clock (`nowFunc`). Nos testes, travamos a execução em uma época estável, eliminando qualquer risco de falhas por trocas de janela temporal.
> - **Serviço de E-mail Resiliente:** O `MailService` agora trata de forma segura e explícita a ausência de SMTP (comportando-se como no-op / log de aviso e retorno `nil`), permitindo testes estáveis sem infraestrutura de e-mail ativa.

A variável `RUN_INTEGRATION_TESTS=true` deve estar definida no CI. Caso contrário, os testes de integração ainda poderão ser pulados durante uma execução aparentemente verde.

## Ordem prática para os próximos 30 dias

| Período | Entrega principal |
|---|---|
| Dias 1–3 | Teste completo em produção, mobile, HMAC, retry, logs e alertas |
| Dias 4–7 | Correção dos problemas encontrados e criação de smoke tests |
| Semana 2 | Métricas de ativação, página de status e documentação de segurança |
| Semana 3 | Recrutamento de três a cinco pilotos e acompanhamento do primeiro job |
| Semana 4 | Primeira oferta paga, coleta de feedback e decisão sobre infraestrutura |

## Critério para considerar o CronFlow pronto para vender

Cinco critérios objetivos:

1. Um usuário novo cria e executa o primeiro job sem ajuda manual.
2. Uma falha simulada gera retry, log e alerta corretamente.
3. O usuário consegue entender por que uma execução falhou.
4. O CI bloqueia regressões de frontend, backend e integração.
5. Pelo menos alguns usuários externos demonstram intenção de continuar usando ou pagar.

**Recomendação final:** não adicionar outra máquina agora e não vender o projeto prematuramente por falta de validação. Primeiro validar o fluxo em produção, conquistar pilotos e medir o primeiro sucesso. Depois dos primeiros usuários recorrentes, haverá dados suficientes para decidir entre continuar como SaaS, buscar parceria ou vender por um valor maior e mais justificável.
