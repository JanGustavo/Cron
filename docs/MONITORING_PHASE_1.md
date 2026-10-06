# Monitoramento — primeira etapa implementada

Esta entrega estabelece a avaliação confiável e torna seus resultados consultáveis. A proposta completa continua em `MONITORING_REFACTOR_ANALYSIS.md`; os achados daquele documento descrevem o estado anterior à implementação.

## Comportamento

- O cliente HTTP captura até 256 KiB, com um byte adicional para detectar excesso. A avaliação recebe o corpo completo dentro desse limite; o log continua armazenando prévia de até 2 KiB.
- Erro de leitura e excesso de tamanho são explícitos. A duração inclui a leitura da resposta. Erro de leitura também marca falha da chamada HTTP.
- Cada condição recebe `pass`, `fail`, `error` ou `skipped`, com motivo, campo, operador, expectativa e valor observado. O campo legado `passed` permanece disponível.
- JSON inválido e campo ausente são falhas de contrato; falta de resposta HTTP gera `skipped`, enquanto a falha de transporte é registrada separadamente.
- Comparações numéricas não aceitam texto como alternativa. Números são comparados como decimais exatos, sem arredondamento para float64. O comparador aceita até 128 caracteres por número e expoentes entre -1000 e 1000 para limitar custo de processamento.
- Limites numéricos inválidos são recusados na criação/edição. As configurações legadas ainda armazenam limites textuais; em igualdade, o tipo esperado segue o escalar observado. Um contrato de tipo esperado independente da amostra será uma evolução posterior, com migração explícita.
- Caminhos existentes com pontos permanecem suportados; segmentos numéricos permitem índices em arrays (`items.0.status`). Não há suporte a JSONPath completo nem seleção inequívoca de chaves contendo pontos nesta etapa.
- Respostas de jobs não sobrescrevem mais o cache global de chaves do projeto. A ingestão real permanece no endpoint legado e só consulta regras globais da chave.
- A associação de regras e a simulação de jobs verificam pertencimento ao projeto.

## Prévia sem efeitos colaterais

`POST /v1/monitor/preview`, com autenticação existente:

```json
{
  "job_id": "UUID_DO_JOB",
  "key": "data.errors",
  "value": 0
}
```

`job_id` é opcional: quando ausente, avalia regras globais da chave. Quando presente, avalia regras daquele job e globais aplicáveis, filtrando a chave selecionada. O valor mantém tipo JSON, incluindo `0`, `false`, `""` e `null`. A prévia não grava amostras e não notifica. Ela avalia regras já cadastradas; a prévia de uma condição candidata antes de salvar pertence à próxima evolução do builder.

A tela de monitoramento usa esse endpoint, permite escolher a origem e informa quando nenhuma condição foi encontrada, sem apresentar aprovação indevida. A amostra numérica é enviada sem passar por arredondamento de JavaScript.

## Histórico e interface

As listagens por job e por projeto retornam `rule_evaluations`. O detalhe do log mostra os resultados registrados, inclusive avaliações antigas que só possuem `passed` e mensagem. O valor observado também tem representação textual exata para exibição de números grandes.

O resumo e a linha de tentativas deixaram de inventar DNS, horários, respostas de tentativas anteriores e confirmação de entrega de alertas. Sem um identificador comum de run, somente a tentativa daquele registro é apresentada. Dados atuais do job ainda são exibidos como configuração; o snapshot completo da configuração será introduzido com o modelo de runs.

## Compatibilidade e próximas etapas

Regras existentes continuam sendo de acompanhamento: uma violação não muda automaticamente a política de encadeamento ou repetição da chamada. A próxima etapa introduzirá intenção explícita de condição de sucesso e resultado efetivo de execução.

Campos ausentes e JSON inválido agora podem notificar conforme os canais da regra, em vez de desaparecer da avaliação. Alertas ainda usam o mecanismo existente, sem deduplicação por incidente ou outbox; essa limitação será tratada na etapa de operação contínua. Excesso de tamanho/erro interno são exibidos como erro de avaliação, sem simular valor observado.

Esta entrega não adiciona tabelas. Exige as migrations 015 e 016 já existentes, especialmente a coluna `executions.rule_evaluations`. Backend e frontend precisam ser publicados juntos para disponibilizar `/preview`. A implantação não foi executada nesta tarefa.

## Verificação

- Testes de domínio: falhas de contrato, tipos, zeros/false/null, arrays, limites de captura e precisão numérica.
- Testes de serviço: prévia sem escrita e rejeição de job de outro projeto; avaliação de job sem sobrescrever cache global.
- Testes HTTP com autenticação: amostra tipada, precisão, null, dados ausentes, tamanho máximo e validação de origem.
- Testes de leitura do histórico com driver de teste: avaliações retornadas por ambas as listagens e compatibilidade com execuções antigas contendo SQL NULL.
- Testes de captura: limite exato, excesso, leitura parcial e prévia UTF-8.
- Build TypeScript/Vite e renderização React de estados de monitoramento, duração zero e tentativa real.

O driver de teste valida a conversão e o contrato de leitura; não substitui uma validação integrada com PostgreSQL e execuções HTTP reais em ambiente de homologação.
