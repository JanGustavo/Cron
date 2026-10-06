# Auditoria de upgrade e downgrade — 06/10/2026

## Conclusão

A revogação de recursos Pro **não está consistente**. A confirmação de pagamento libera os limites Pro no backend, mas expiração, downgrade automático e execução de recursos previamente configurados não passam por uma política única.

Resposta ao caso de uma conta com oito jobs ativos:

- **Downgrade por cobrança:** os oito permanecem ativos; nenhum é pausado. O scheduler continua considerando os oito elegíveis.
- **Downgrade pelo administrador:** os cinco jobs ativos mais antigos ficam ativos; os três mais novos são pausados. A seleção considera todos os projetos da conta. Jobs já pausados não entram na seleção. Empates em `created_at` não têm desempate explícito.
- **Depois da pausa:** a API permite reativar os excedentes e voltar a oito jobs ativos no Free.
- **Fim do período Pro:** a data de vencimento não participa da decisão de acesso; uma assinatura `active` vencida ainda recebe Pro.
- **Recursos existentes:** workflows e alertas webhook podem continuar funcionando, pois a execução não revalida o direito ao recurso.

Não foram alteradas assinaturas, planos, pagamentos ou jobs reais nesta auditoria. Foram adicionados apenas diagnóstico reproduzível e documentação. A funcionalidade de foto/modal não foi modificada.

## Método e limites da evidência

1. Leitura dos caminhos de checkout, webhook, administração, resolução de limites, criação/reativação de jobs, scheduler, worker, monitoramento, retenção e atualização do frontend.
2. Consulta somente de leitura em produção, com saída agregada, sem dados pessoais nem credenciais.
3. Banco PostgreSQL descartável com todas as migrations. Os testes chamam os repositórios, serviço de jobs e handler Asaas reais, com eventos sintéticos locais. Nenhuma chamada ao provedor financeiro ou endpoint HTTP de job é feita.
4. A bateria também executa testes existentes de retenção, avatar e persistência do monitoramento.

As observações do worker, dos alertas e do frontend abaixo são evidências de código; não foram enviados alertas reais nem simuladas transações reais. O resultado `PASS` do teste de auditoria significa que o diagnóstico executou sem erro técnico, **não** que as políticas comerciais estão corretas. Ele registra o comportamento observado para comparação depois da correção.

Resultados completos: [billing-lifecycle-audit-results.json](assets/billing-lifecycle-audit-results.json).

## Estado agregado de produção

| Item | Resultado |
| --- | --- |
| Free | 5 jobs; 7 dias de logs; sem workflows, alertas webhook ou múltiplos projetos nos limites do plano |
| Pro | 50 jobs; 90 dias de logs; os três recursos habilitados |
| Contas Free | 3; nenhuma acima de 5 jobs ativos; máximo de 18 jobs armazenados em uma conta |
| Contas Pro | 2; uma com assinatura ativa e uma sem assinatura |
| Assinatura ativa com período vencido | 1 |
| Eventos registrados sem processamento / com erro | 0 / 0 |

A assinatura vencida requer reconciliação com o Asaas antes de corrigir a conta: a consulta local não comprova ausência de renovação ou pagamento no provedor. Pro sem assinatura também não deve ser revogado indiscriminadamente, pois pode representar concessão administrativa legítima.

## Cenários reproduzidos

| Cenário | Comportamento observado | Avaliação |
| --- | --- | --- |
| Checkout pendente | Free, máximo 5 | Não concede Pro antes da confirmação |
| `PAYMENT_CONFIRMED` | Pro, máximo 50, no término do handler | Upgrade funciona no backend quando encontra a assinatura local |
| `PAYMENT_OVERDUE` com assinatura localizada | Free; 8 jobs ativos | Limites mudam, excedentes não são pausados |
| `PAYMENT_REFUNDED` | Free; 8 jobs ativos | Mesmo problema |
| `PAYMENT_CHARGEBACK_REQUESTED` | Free; 8 jobs ativos | Mesmo problema |
| `SUBSCRIPTION_DELETED` | Free; 8 jobs ativos | Mesmo problema; cancela o acesso imediatamente, sem preservar o período restante |
| Scheduler depois do downgrade automático | 8 jobs elegíveis | Execução não aplica o máximo 5 |
| Configurações existentes depois do downgrade | Encadeamento e webhook preservados | Preservação é útil, mas falta suspensão efetiva do recurso |
| Downgrade administrativo | Jobs 1–5 ativos; 6–8 pausados | Usa os cinco ativos mais antigos |
| Reativação no Free | Volta a 8 ativos | Bypass da cota |
| Criação de novo job com 8 armazenados | Bloqueada | Conta jobs armazenados, inclusive pausados |
| Pro `active` com período encerrado e cancelamento ao fim do período | Continua Pro | Expiração ignorada |
| Cancelamento ao fim do período com data futura | Continua Pro | Preserva acesso agora, mas não há transição quando a data vence |
| Reentrega de evento registrado com erro | Marcada como duplicada | Não tenta recuperar processamento falho |
| Recebimento antigo após estorno mais recente | Volta a Pro | Não verifica precedência/identidade da obrigação paga |

## Achados e causas

### 1. Falta uma transição única de plano — prioridade alta

`BillingRepository.UpsertSubscription` atualiza assinatura e `users.plan` em duas operações, sem transação. O handler Asaas utiliza esse método, mas não pausa excedentes. A administração tem SQL separado que pausa jobs, com limite 5 fixo e erros da assinatura/pausa ignorados. `PauseExcessJobsForUser` existe, mas não tem chamadores.

Fontes: `internal/repository/postgres/billing_repository.go:190`, `internal/repository/postgres/user_repository.go:424`, `internal/repository/postgres/job_repository.go:323`, `internal/api/handler/billing_handler.go:339`.

Consequência: o mesmo destino Free tem efeitos diferentes conforme a origem da mudança, e uma falha parcial pode deixar dados divergentes.

### 2. Expiração e concessão manual estão misturadas — prioridade alta

`EntitlementEngine.GetUserLimits` considera apenas `active`/`trialing`; não verifica `current_period_end`. Além disso, `GetSubscription` força `active`/Pro quando `users.plan='pro'`, mesmo se a assinatura armazenada estiver cancelada. O mesmo campo representa tanto plano pago espelhado quanto concessão manual. Não foi encontrado reconciliador periódico de assinaturas vencidas.

Fontes: `internal/service/billing_service.go:31`, `internal/repository/postgres/billing_repository.go:21`.

Consequência: o prazo pode vencer sem revogação. A retenção de logs acompanha essa mesma classificação, mantendo 90 dias enquanto a conta permanecer efetivamente Pro.

### 3. Reativação e execução não conferem cotas — prioridade alta

`JobService.UpdateStatus` verifica ownership e recalcula agenda, mas não consulta plano nem quantidade ativa. O scheduler filtra status, falhas e horário; não consulta limites. O worker verifica status do job, mas não resolve direitos da conta antes de executar ou encadear o próximo job.

Fontes: `internal/service/job_service.go:158`, `internal/repository/postgres/job_repository.go:209`, `internal/worker/worker.go`.

A criação tem trava transacional para cota; a reativação não compartilha essa proteção. Também há uma divergência conceitual: `CountActiveJobs` conta todos os jobs armazenados, apesar do nome, enquanto o downgrade administrativo limita apenas ativos.

### 4. Recursos Pro existentes continuam executáveis — prioridade alta

Criação/edição de jobs bloqueiam novos workflows e webhooks no Free. A execução de configurações salvas não revalida essas permissões. Regras de monitoramento também despacham alertas sem resolver o plano; e-mail de violação verifica preferência do usuário, mas não o Pro. O e-mail comum de falha de job possui checagem de `users.plan`, portanto os caminhos são diferentes.

Fontes: `internal/worker/worker.go:159`, `internal/worker/worker.go:178`, `internal/service/monitor_service.go:168`, `internal/service/alert_service.go:403`.

É correto preservar configurações para um futuro upgrade; é necessário impedir o uso de recursos indisponíveis sem apagar essas configurações.

### 5. Webhooks não têm recuperação e ordenação suficientes — prioridade alta

Qualquer evento já registrado é ignorado como duplicado, inclusive se teve erro de processamento. Falhas ao marcar o processamento apenas são registradas no log. Eventos de pagamento sem assinatura correspondente recebem sucesso, sem registrar erro recuperável. Atraso/estorno e confirmação/recebimento não têm controle para evitar que um evento antigo substitua um estado mais recente.

Fontes: `internal/service/billing_service.go:65`, `internal/api/handler/billing_handler.go:339–457`.

A integração calcula o período a partir do instante em que **cada evento** chega e trata valor acima de R$150 como anual. Assim, `PAYMENT_CONFIRMED` e `PAYMENT_RECEIVED` da mesma cobrança podem recalcular o período duas vezes, em datas distintas. Isso precisa usar dados explícitos da contratação e período pago, sem inferir ciclo pelo valor.

O Asaas documenta ambos os eventos como etapas distintas da mesma cobrança. Referência: [eventos para cobranças](https://docs.asaas.com/docs/webhook-para-cobrancas). Também documenta entrega pelo menos uma vez e necessidade de idempotência: [boas práticas gerais](https://docs.asaas.com/docs/boas-pr%C3%A1ticas-gerais).

### 6. Abrir um novo checkout pode retirar o Pro atual — prioridade alta

Ao criar outro link, o handler altera a assinatura existente para `pending`; `UpsertSubscription` então escreve `users.plan='free'`, mesmo se ainda havia período Pro vigente. Esse achado foi identificado por leitura de código, sem criar checkout no Asaas.

Fonte: `internal/api/handler/billing_handler.go:100–118`.

Checkout pendente precisa ser uma intenção de compra separada, sem substituir a assinatura efetiva.

### 7. O frontend pode exibir o plano anterior — prioridade média

Os jobs são atualizados a cada 10 segundos, mas o perfil/limites são sincronizados apenas ao autenticar, trocar projeto ou mudar `jobs.length`. Uma alteração de status/assinatura sem mudança na quantidade de jobs não dispara essa atualização. Portanto, o backend pode liberar Pro enquanto a tela continua bloqueada, ou revogar recursos enquanto a tela ainda anuncia Pro.

Fonte: `../cron front/src/App.tsx:397–429`.

### 8. Projetos e validações têm restrições incompletas — prioridade média

Criar projeto consulta `MultiProjectEnabled`; alternar entre projetos existentes verifica somente ownership. Jobs em projetos antigos continuam na seleção do scheduler. Não há conceito de projeto principal autorizado no Free.

No monitoramento, a interface permite uma condição por job no Free; o handler/serviço de criação não aplica essa cota. As regras existentes continuam sendo avaliadas. Essa restrição comercial precisa ser definida no backend, com cuidado para não remover silenciosamente condições obrigatórias de sucesso.

Fontes: `internal/api/handler/auth_handler.go:1513`, `internal/api/handler/auth_handler.go:1709`, `internal/api/handler/monitor_handler.go:127`, `internal/service/monitor_service.go:103`, `../cron front/src/components/Kanban/JobMonitorRulesPanel.tsx:100`.

### 9. Aviso prévio existe, mas não está conectado — prioridade média

`SendDowngradeWarningEmail` existe sem chamadores. Não há envio comprovado do aviso três dias antes de perder Pro, nem fluxo unificado que informe quais jobs foram pausados e por quê.

Fonte: `internal/service/mail_service.go:504`.

## Política proposta para a correção

Esta seção é uma proposta; não foi aplicada às contas reais.

1. **Plano efetivo único:** derivar acesso de assinatura válida, período pago e concessão manual explícita com origem/prazo próprios. Todas as rotas e processos usam essa decisão. Reconciliação periódica recupera webhooks perdidos; a checagem de execução protege enquanto a reconciliação não terminou.
2. **Upgrade:** confirmação válida libera os recursos assim que persistida. Frontend atualiza ao voltar do checkout, ganhar foco e em intervalo limitado. Jobs pausados manualmente continuam pausados; a retomada de jobs suspensos por plano fica explícita, para evitar execuções inesperadas.
3. **Downgrade normal:** mantém Pro até o fim do período pago. Estorno/chargeback e atraso precisam de regras distintas e explícitas; período de tolerância para atraso é decisão comercial, não pressuposto técnico.
4. **Cinco jobs ativos por conta:** preservar os jobs armazenados; manter cinco ativos escolhidos pelo usuário antes da mudança. Sem escolha, manter os cinco ativos mais antigos com desempate por ID. Registrar `pause_reason=plan_limit`. Permitir trocar um ativo por outro preservado, sob uma única trava transacional. Explicitar separadamente o limite de armazenamento para novas criações.
5. **Recursos Pro preservados e suspensos:** guardar workflows/webhooks/preferências, mas conferir direitos no momento de execução/despacho. Identificar os recursos indisponíveis na interface. Para validações obrigatórias excedentes, suspender o job ou oferecer adequação explícita; não descartá-las e passar a aceitar respostas que antes eram inválidas.
6. **Projetos:** definir projeto principal no Free e comportamento dos demais; oferecer consulta/exportação dos dados e transferência de jobs. Esta decisão precisa anteceder bloqueios de execução em projetos antigos.
7. **Eventos confiáveis:** persistir ID original, identificar cobrança/período, processar transição em transação, permitir retry após falha e impedir regressão por evento antigo. Duas etapas da mesma cobrança não compram dois períodos.
8. **Retenção:** manter 7 dias Free/90 Pro conforme a configuração atual. Ao efetivar downgrade, explicar que o próximo expurgo pode remover logs fora da janela de 7 dias; a limpeza roda no início do scheduler e aproximadamente a cada hora. Regras de retenção e plano efetivo devem continuar alinhadas.
9. **Experiência:** resumo antes do downgrade com data, cinco jobs selecionados, excedentes e recursos afetados; depois, toast/notificação com o resultado. A cobrança segue no backend, independentemente da tela estar aberta.

## Ordem sugerida de implementação

1. Separar assinatura, checkout pendente e concessão manual; centralizar plano efetivo, prazo e transição atômica.
2. Aplicar limite na transição e reativação; registrar motivo de pausa; conferir permissões no scheduler/worker/alertas.
3. Corrigir idempotência, retry, período pago e precedência dos eventos; adicionar reconciliação com o provedor.
4. Sincronizar frontend e implementar resumo/seleção/notificações, com testes das políticas comerciais adotadas.

Antes de ativar a reconciliação em produção, obter um relatório de diferenças com o Asaas e preservar as concessões administrativas. A conta com período local vencido não foi alterada por esta auditoria.

## Reprodução

```bash
# PostgreSQL 16 e Go locais. Banco temporário via socket Unix; sem rede/provedor.
GOCACHE=/tmp/cronflow-monitor-go-cache GOPROXY=off bash scripts/audit-billing-lifecycle.sh
```

O script substitui o DSN de teste por um cluster descartável próprio e encerra/remove o cluster ao sair. `CRONFLOW_PG_BIN` permite informar outro diretório dos binários PostgreSQL. O JSON de evidências é regravado em `docs/assets/billing-lifecycle-audit-results.json`.
