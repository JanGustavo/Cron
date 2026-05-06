package middleware

// AuthMiddleware intercepta toda request autenticada.
// Fluxo:
//   1. Ler header: Authorization: Bearer <api_key>
//   2. Calcular SHA-256(api_key)
//   3. Consultar UserRepository.FindProjectByKeyHash(hash)
//   4. Inválido → 401. Válido → injeta Project no context da request.
// Nenhum Handler faz autenticação — ela vem resolvida no context.
