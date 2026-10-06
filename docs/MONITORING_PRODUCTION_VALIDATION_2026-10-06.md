# Publicação e validação em produção — 6 de outubro de 2026

## Entrega

API, worker e frontend publicados em `https://cronflow.app` e `https://api.cronflow.app`. O health check público identifica a API como `monitoring-20261006`, com PostgreSQL e Redis disponíveis. O scheduler permaneceu ativo.

Aplicadas atomicamente as migrations 016 (complemento idempotente da coluna de avaliações que faltava em produção) e 017 (modo, tipo e status de transporte). A migration 017 preencheu o status de transporte das execuções existentes. A conferência de limpeza também encontrou uma instalação antiga de `job_id` sem sua chave estrangeira. Foi adicionada a migration 018, testada antes em PostgreSQL isolado simulando esse estado. Em produção, o único vínculo órfão era a regra criada no teste; ela foi removida e a chave estrangeira `ON DELETE CASCADE` foi aplicada e validada. Nenhuma regra de cliente foi removida.

Não havia tabela `schema_migrations` neste ambiente; foram usados os arquivos SQL do projeto dentro de uma transação.

Antes da alteração, foram guardados o dump completo do banco, os binários anteriores, o frontend e a configuração do upstream em `/opt/cronflow/backups/monitoring-20261006`, no servidor. O diretório possui acesso restrito. A credencial fornecida para o teste permaneceu somente na memória do processo de validação; não foi salva neste relatório nem nos scripts, comandos de shell ou logs.

A primeira troca foi revertida automaticamente quando uma checagem pública imediata ainda encontrou a API anterior durante o reload do Nginx. A segunda troca aguardou a propagação do reload e confirmou a versão nova externamente. A API blue atende na porta 8080, a instância green foi parada e o serviço legado da API foi desabilitado para evitar conflito com a instância blue no próximo boot. A instância blue foi habilitada no systemd. As demais configurações do Nginx foram preservadas.

O índice e os assets publicados foram comparados por SHA-256 com o build local. Os binários publicados têm estes hashes:

- API: `66679a731af6de353270d0027dd2b86397ed3788914713def4d8267252029faf`
- Worker: `fc89f551864feca5d7b95ada19cc8656cabca3b3b41df914c1be72a78ad2ef90`

## Testes reais

Foram criados dois jobs temporários com agendamento anual futuro, executados manualmente. Usaram respostas JSON estáticas e públicas, sem dados de clientes, hospedadas temporariamente no próprio domínio. Nenhum endpoint de negócio existente foi disparado. As condições de teste tinham envio de e-mail desabilitado e não tinham webhook de alerta; o projeto não possuía condições globais ativas que enviassem notificações.

| Cenário | Evidência |
|---|---|
| Prévia candidata de requisito não atendido | `passed=false`, avaliação `fail`, quantidade de regras persistidas inalterada |
| Resposta HTTP válida e requisito atendido | `success`, transporte `success`, avaliação `pass` e execução do job seguinte |
| HTTP 200 com `synced=false` e requisito obrigatório | `validation_failed`, transporte `success`, avaliação `fail`, próximo job não executado, job ativo e contador de falhas HTTP zerado |
| Mesma resposta em modo acompanhar | Execução `success`, violação registrada como `fail` |
| Chamada HTTP sem autenticação a uma rota protegida | HTTP 401 real, execução `failed` e transporte `failed`, distintos de uma falha de validação |

O job com falha HTTP foi pausado imediatamente para impedir repetições durante o teste. Os dois jobs e seu histórico temporário foram removidos via API depois da validação. A regra órfã do teste foi removida na transação de correção da integridade descrita acima. O banco confirmou zero jobs e zero regras do teste; não restaram tarefas pendentes ou de retry dos IDs temporários. Foram conferidos também banco e fila para evitar resíduos dos testes.

Os pop-ups e o fluxo de edição/prévia foram verificados previamente no navegador com fixture isolada, incluindo uma nova execução recebida pelo acompanhamento periódico. Nesta validação de produção, foi confirmado o frontend publicado por hash; não houve sessão de usuário aberta no navegador para observar pop-ups de produção.

## Reversão

O backup permanece no servidor. As migrations são aditivas e compatíveis com os binários anteriores. Uma reversão dos binários não exige desfazer imediatamente o banco. A reversão de schema usa os arquivos `.down.sql`, com as perdas semânticas descritas em `MONITORING_PHASE_2.md`, e deve ser tratada separadamente.

O teste não validou envio real de e-mail/webhook, pois esses canais foram deliberadamente desabilitados nas condições temporárias. Não foram implementadas notificações push para navegador fechado.
