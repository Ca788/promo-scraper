# promo-scraper — Stack Card (backend/go)

> Este projeto segue a stack **backend/go** do spec-engine.
> Convenções completas em [`.spec/stacks/backend/go/CONVENCOES.md`](.spec/stacks/backend/go/CONVENCOES.md).
> SDD: [`.spec/VISAO.md`](.spec/VISAO.md), [`.spec/especificacoes/`](.spec/especificacoes/).

---

## Stack

- **Linguagem:** Go 1.23+
- **HTTP:** chi/v5
- **DB:** Postgres via pgx/v5 + pgxpool
- **Query layer:** sqlc (SQL como fonte da verdade)
- **Migrations:** golang-migrate
- **Validation:** go-playground/validator/v10
- **Auth:** chi/jwtauth (JWT)
- **Rate limit:** chi/httprate
- **Logs:** log/slog (stdlib)
- **Queue:** river ou asynq
- **Testes:** stdlib testing + testify

---

## Princípios não-negociáveis

1. **Handler thin** — decode, validate, chama Use Case, escreve envelope.
2. **Use Case** = struct + `Execute(ctx, input) (output, error)`. Sem `Service` faz-tudo.
3. **`context.Context` em toda função de IO.**
4. **Repository interface no `domain/`**, impl em `infrastructure/`.
5. **sqlc para queries**, nunca `db.Query("SELECT ...")` solto.
6. **Errors como valores tipados**; mapeamento HTTP em `shared/apierror`.
7. **OrgID filtrado em toda query** (multi-tenant).
8. **Envelope JSON canônico** (`shared/envelope`).
9. **Config validada na startup** — falha rápido se inválida.
10. **Sem panic em produção.** Errors são retornados.

---

## Estrutura

```
cmd/
├── server/main.go              chi router + middlewares
└── worker/main.go              (opcional) jobs
internal/
├── config/                     Load() valida env
├── shared/{apierror,envelope,middleware,logger,pagination}/
└── modules/<bounded>/<entity>/
    ├── domain/                 entity, repository interface, errors
    ├── application/            use cases
    ├── infrastructure/         pg repository + sqlc queries.sql
    └── interface/              http handler + dto
db/
├── migrations/                 000001_xxx.up.sql + .down.sql
└── queries/                    .sql lidos por sqlc
sqlc.yaml
```

---

## Convenções de nomenclatura

| Item | Padrão | Exemplo |
|---|---|---|
| Pacote | snake-lowercase | `transactions` |
| Arquivo | snake_case | `create_transaction.go` |
| Struct | PascalCase | `CreateTransaction`, `Transaction` |
| Função/método | PascalCase exportado, camelCase privado | `Execute`, `mapToDomain` |
| Constante | PascalCase ou ALL_CAPS quando crítica | `StatusPending`, `MaxRetries` |
| Interface | PascalCase, geralmente substantivo + "-er" ou objeto | `Repository`, `IdempotencyStore` |
| Erro var | `ErrXxx` | `ErrNotFound`, `ErrInvalidAmount` |

---

## Comandos canônicos

### Operação

```bash
go run ./cmd/server
go test ./... -race -cover
golangci-lint run
gofmt -s -w . && goimports -w .
govulncheck ./...
```

### Generators (obrigatórios)

| Tarefa | Comando |
|---|---|
| Nova migração | `migrate create -ext sql -dir db/migrations -seq <nome>` |
| Aplicar migrações | `migrate -path db/migrations -database $DATABASE_URL up` |
| Regenerar sqlc | `sqlc generate` |
| Mocks (interfaces) | `mockery --all --keeptree --output mocks/` |
| Adicionar dep | `go get github.com/foo/bar@v1.2.3 && go mod tidy` |

---

## Proibições (revisor reprova)

- ❌ Handler chamando `pgxpool.Pool` direto.
- ❌ Use Case importando `chi` ou `pgx`.
- ❌ `panic(err)` em código de produção.
- ❌ Goroutine sem `context.Context`.
- ❌ Função IO sem `ctx` no 1º argumento.
- ❌ Query `SELECT *` sem `LIMIT` em endpoint público.
- ❌ Loop chamando `repo.Find...` (N+1) — use JOIN no sqlc.
- ❌ `time.Now()` em domain — injete `Clock`.
- ❌ Logger sem `request_id` + `org_id`.
- ❌ Resposta sem envelope canônico.
- ❌ Variável global mutável.
- ❌ String SQL concatenando user input.
- ❌ `AllowOrigin: "*"` em produção.
- ❌ Criar `.sql` à mão para query que sqlc cobriria.

---

## Performance, rate limit, concorrência, regressão

- **Performance:** `EXPLAIN ANALYZE`, índices compostos, `pgxpool` dimensionado, `GOMEMLIMIT` em container, métrica `http_request_duration_seconds`.
- **Rate limit:** `httprate` global + por usuário em endpoints sensíveis; resposta 429 envelopada + `Retry-After`.
- **Concorrência:** `Idempotency-Key` + tabela; transação via `Repository.WithTx`; optimistic locking com coluna `version`; `errgroup` para paralelismo controlado; CI roda `go test -race`.
- **Regressão:** todo bug vira teste de request **antes** do fix.

Detalhes: seções 12–15 de [`CONVENCOES.md`](.spec/stacks/backend/go/CONVENCOES.md).
