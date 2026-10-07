# Reconciliação Asaas — 7 de outubro de 2026

A concessão de Pro agora depende de consultas autenticadas ao estado atual das cobranças do Asaas. O webhook é registrado antes do processamento, com idempotência pelo ID original; alterações da assinatura, da tolerância de jobs e a confirmação do evento são transacionais. Eventos antigos consultam o estado atual e não restauram uma cobrança já estornada nem removem uma renovação válida.

Os links de checkout são vinculados à conta em um registro próprio. Uma intenção de compra não substitui acesso pago nem concessões administrativas. O ciclo mensal/anual vem do contrato ou do link; descontos e preços não determinam duração. O período começa na maior data entre vencimento e pagamento, usando calendário de São Paulo e ajuste para o último dia do mês. CONFIRMED e RECEIVED não concedem dois períodos. Um cancelamento preserva um período já pago até seu término.

Falhas de verificação permanecem registradas para repetição local, com atraso de 60 segundos até uma hora. Depois da gravação durável, o webhook retorna 200 para não bloquear a fila do provedor; falha na gravação retorna 503. O scheduler verifica eventos pendentes a cada minuto e reconcilia contas na inicialização e a cada 15 minutos. Referências ausentes ou dados insuficientes não inventam renovação; concessões manuais permanecem fora dessa reconciliação.

A política unificada continua: ao terminar Pro, recursos pagos e IA são bloqueados; quem excede cinco jobs ativos recebe 48 horas para escolher quais pausar. Ao fim, permanecem os cinco ativos mais antigos da conta. Renovação encerra a tolerância, sem reativar automaticamente jobs pausados.

## Validação

Bateria em PostgreSQL descartável: duplicação concorrente, confirmação/liquidação, atraso e desconto, ciclo anual, estorno, renovação posterior, entrega fora de ordem, indisponibilidade e reprocessamento, vínculo de checkout, prioridade administrativa, snapshots antigos e rollback transacional. Inclui testes de tolerância, retenção, avatar e monitoramento. Migrations 021 e 022 são revertidas e reaplicadas no ambiente isolado. CI executa esses testes com o banco de teste configurado.

Na auditoria de produção, a chave Asaas respondeu, o link pendente existia, uma referência legada de assinatura retornou 404 e o único webhook apontava para api.cronflow.jangustavo.me, domínio que não resolvia. O destino correto é https://api.cronflow.app/v1/billing/webhook. A referência 404 requer investigação de histórico; não é substituída por uma assinatura fictícia. Não foram criadas cobranças ou alteradas assinaturas reais. Testes financeiros usam provedor simulado; consultas e configuração do receptor usam o Asaas real.

## Referências

- [Idempotência de webhooks](https://docs.asaas.com/docs/como-implementar-idempotencia-em-webhooks)
- [Eventos de cobranças](https://docs.asaas.com/docs/webhook-para-cobrancas)
- [Eventos de assinaturas](https://docs.asaas.com/docs/eventos-para-assinaturas)
- [Listar cobranças](https://docs.asaas.com/reference/listar-cobrancas)
- [Atualizar webhook existente](https://docs.asaas.com/reference/atualizar-webhook-existente)
