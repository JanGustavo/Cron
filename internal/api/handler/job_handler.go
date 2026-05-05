package handler

// JobHandler — handlers HTTP para o recurso Job.
// Responsabilidade: deserializar request → chamar JobService → serializar response.
// Handlers são THIN: zero lógica de negócio aqui.
// Erros retornados no formato RFC 7807 (Problem Details).

type JobHandler struct{}
