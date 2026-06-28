---
artefato: passo
cenario_local: ../cenarios/detectar-queda-de-preco-em-fonte-http/CENARIO.md
numero: 01
fase: Bootstrap + módulo `sources` (domínio + persistência)
depende_de: []
criado_em: 2026-06-28T17:00:36Z
---

# Passo 01 — Bootstrap + módulo `sources` (domínio + persistência)

> **Contexto do projeto:** veja `CLAUDE.md` na raiz do repositório.
>
> **Para o agente de execução:** este Passo é uma **fatia vertical** — agrupa todos os artefatos necessários para entregar a fase de ponta a ponta (migration + sqlc + domínio + repositório + testes, juntos, não como Passos separados). Se você não consegue apontar arquivos específicos para criar ou modificar, este Passo está vago demais — recuse e peça refinamento. **TDD é obrigatório:** escreva o teste primeiro, código mínimo para passar, depois refatore. Testes moram neste Passo, não em Passo seguinte.
>
> **Leia nesta ordem antes de escrever código:**
> 1. O `CENARIO.md` pai (`../cenarios/detectar-queda-de-preco-em-fonte-http/CENARIO.md`)
> 2. A `ESPEC.md` avó (`../ESPEC.md`)
> 3. O `DESENHO.md` irmão da ESPEC (`../DESENHO.md`) — schemas SQL canônicos estão lá
> 4. Os arquivos listados em **Contexto a ler antes** abaixo
> 5. O `CLAUDE.md` da raiz (para convenções da stack Go)

**Fase:** Bootstrap + módulo `sources` (domínio + persistência)
**Depende de:** Nenhum
**Atrás de feature flag:** Não — worker ainda não está em produção

## 📋 Descrição técnica

**Objetivo deste passo:** estabelecer a fundação de persistência da capacidade `coleta-de-promo`. Criar o esquema relacional para `stores` e `sources`, gerar queries tipadas via `sqlc`, expor `SourceRepository` como interface no domínio, implementá-la em `infrastructure/`, e cobrir tudo com testes unitários (domínio) e de integração (repositório contra Postgres real).

**Comportamento atual:** o repositório só contém `go.mod`, `main.go` "hello world", `docker-compose.yml` (Postgres + Redis) e `CLAUDE.md`. Não existem migrations, módulos, ou estrutura `internal/`.

**Comportamento esperado após este passo:**
- `sqlc.yaml` configurado, gerando código a partir de `db/queries/` contra os types em `db/migrations/`.
- Migrations `golang-migrate` aplicáveis criando `stores` e `sources` exatamente como definidas no `DESENHO.md`, incluindo índice `(enabled, last_collected_at)` para o scheduler e UNIQUE `(org_id, url)`.
- Pacote `internal/modules/scraping/sources/domain/` expõe a entidade `Source`, o value object `Snapshot` com método `HasPriceDrop(novo decimal.Decimal) bool` (≥ 1 centavo), a interface `SourceRepository` e os erros tipados `ErrSourceNotFound` e `ErrConcurrentUpdate`.
- Pacote `internal/modules/scraping/sources/infrastructure/` implementa o repositório consumindo as queries geradas pelo sqlc, com `WithTx` para compor transação.
- `GetEligibleSources` retorna apenas fontes `enabled=true` cujo `now() >= last_collected_at + interval_seconds * interval '1 second'` (ou `last_collected_at IS NULL`).
- `UpdateSourceAfterCollect` usa optimistic locking via coluna `version` — falha com `ErrConcurrentUpdate` se o `version` informado divergir do persistido.
- Suite de testes verde via `go test ./... -race -cover`.

## 📁 Arquivos a criar ou modificar

| Ação | Caminho do arquivo | O que fazer |
|---|---|---|
| Criar | `sqlc.yaml` | Configurar engine `postgresql`, schema = `db/migrations/`, queries = `db/queries/`, gen Go em `internal/sqlc/` ou `internal/modules/scraping/sources/infrastructure/sqlc/` (escolher um padrão consistente para o repo) |
| Criar | `db/migrations/000001_init_stores_sources.up.sql` | `CREATE TABLE stores (id bigserial PK, org_id uuid NOT NULL, nome text NOT NULL, host text NOT NULL, created_at timestamptz, updated_at timestamptz, UNIQUE(org_id, host))`; `CREATE TABLE sources (...)` exatamente conforme schema do `DESENHO.md` (`id`, `org_id`, `store_id` FK, `url`, `strategy CHECK IN ('http','headless')`, `interval_seconds`, `selectors jsonb`, `enabled`, `last_collected_at`, `last_snapshot jsonb`, `last_error text`, `version int NOT NULL DEFAULT 1`, timestamps); `UNIQUE(org_id, url)`; índice `(enabled, last_collected_at)` |
| Criar | `db/migrations/000001_init_stores_sources.down.sql` | `DROP TABLE sources; DROP TABLE stores;` |
| Criar | `db/queries/sources.sql` | Queries anotadas para sqlc: `-- name: GetEligibleSources :many` (filtra `enabled = true AND (last_collected_at IS NULL OR now() >= last_collected_at + (interval_seconds \|\| ' seconds')::interval)` com `LIMIT $1`); `-- name: GetSourceByID :one`; `-- name: UpdateSourceAfterCollect :execrows` (`UPDATE sources SET last_snapshot=$1, last_collected_at=now(), last_error=NULL, version=version+1, updated_at=now() WHERE id=$2 AND org_id=$3 AND version=$4`); `-- name: MarkSourceError :exec` (`UPDATE sources SET last_error=$1, last_collected_at=now(), version=version+1, updated_at=now() WHERE id=$2 AND org_id=$3`) |
| Criar | `internal/modules/scraping/sources/domain/source.go` | Struct `Source` (campos correspondentes à tabela, com `Snapshot *Snapshot`); struct `Snapshot` (Preco `decimal.Decimal`, EstoqueDisponivel `bool`, BadgePromo `bool`, Titulo `string`, SKU `string`, ColetadoEm `time.Time`); método `(s *Snapshot) HasPriceDrop(novo decimal.Decimal) bool` — retorna `novo.LessThan(s.Preco)` quando `s` não-nil; quando snapshot é `nil`, retorna `false` (primeiro poll não dispara evento) |
| Criar | `internal/modules/scraping/sources/domain/repository.go` | `package domain`; `var ErrSourceNotFound = errors.New("source not found"); var ErrConcurrentUpdate = errors.New("source version conflict")`; `type SourceRepository interface { GetEligible(ctx, orgID, limit) ([]Source, error); GetByID(ctx, orgID, id) (Source, error); UpdateAfterCollect(ctx, orgID, id int64, version int, snapshot Snapshot) error; MarkError(ctx, orgID, id, msg) error; WithTx(ctx, fn func(SourceRepository) error) error }` |
| Criar | `internal/modules/scraping/sources/domain/source_test.go` | Testes unitários (stdlib `testing` + `github.com/stretchr/testify/require`): `TestSnapshot_HasPriceDrop` com tabela: queda 100→99,99 (true); estável 100→100 (false); alta 100→100,01 (false); snapshot nil + qualquer preço (false) |
| Criar | `internal/modules/scraping/sources/infrastructure/pg_repository.go` | `type PGSourceRepository struct { q *sqlc.Queries; pool *pgxpool.Pool }`; `func New(pool) *PGSourceRepository`; implementa interface mapeando para `sqlc`; em `UpdateAfterCollect` checa `rowsAffected == 0 → ErrConcurrentUpdate`; `WithTx` abre transação `pool.BeginTx`, retorna nova instância com `q = sqlc.New(tx)` |
| Criar | `internal/modules/scraping/sources/infrastructure/pg_repository_test.go` | Integração contra Postgres do `docker-compose.yml`: setup com `pgxpool` + migrate up + truncate antes de cada caso; `TestGetEligibleSources_ReturnsOnlyEligible` (insere 3 fontes: 1 elegível, 1 desabilitada, 1 dentro do intervalo → retorna 1); `TestUpdateSourceAfterCollect_OptimisticLocking` (concorrência simulada: 2 updates com mesma `version` → o 2º recebe `ErrConcurrentUpdate`) |

> **Nota sobre dependências Go:** ao final deste Passo, `go.mod` deve incluir `github.com/jackc/pgx/v5`, `github.com/shopspring/decimal`, `github.com/stretchr/testify` e o pacote gerado pelo sqlc. Adicionar via `go get` na ordem em que cada arquivo é escrito (TDD).

## 📚 Contexto a ler antes

| Caminho do arquivo | Por que importa |
|---|---|
| `CLAUDE.md` | Stack, convenções de nomenclatura, comando de teste, proibições (sem `panic`, sem `time.Now()` em domain, sem `SELECT *`) |
| `.spec/VISAO.md` | Métrica do norte (latência) — informa expectativas de performance da query do scheduler |
| `.spec/especificacoes/coleta-de-promo/ESPEC.md` | Restrições inegociáveis (idempotência, multi-tenant) |
| `.spec/especificacoes/coleta-de-promo/DESENHO.md` | Schema canônico de `sources` (seção "Interfaces e contratos chave") + decisão sobre `last_snapshot` como `jsonb` na própria tabela |
| `.spec/especificacoes/coleta-de-promo/cenarios/detectar-queda-de-preco-em-fonte-http/CENARIO.md` | Critérios BDD que indiretamente derivam comportamento do repositório |
| `docker-compose.yml` | Configuração do Postgres local (credenciais, porta) para testes de integração |
| `go.mod` | Versão do Go (1.26+) e módulo (`promo-scraper`) |

## 🧪 Plano TDD

> Testes funcionam como prompts: um teste é uma especificação em linguagem natural que guia o agente ao comportamento exato.
>
> Testes moram **neste** Passo, nunca em Passo seguinte.

**Ciclo Vermelho/Verde/Refatorar:**

- [ ] 🔴 Escrever `TestSnapshot_HasPriceDrop` (caminho feliz + estável + alta + nil) — deve falhar (método não existe)
- [ ] 🟢 Implementar `Snapshot.HasPriceDrop` no `source.go` — mínimo para passar
- [ ] 🔴 Escrever `TestGetEligibleSources_ReturnsOnlyEligible` (3 fontes inseridas: elegível / desabilitada / dentro do intervalo) — deve falhar (query/repo não existem)
- [ ] 🟢 Definir query sqlc + gerar código + implementar `PGSourceRepository.GetEligible` — mínimo para passar
- [ ] 🔴 Escrever `TestUpdateSourceAfterCollect_OptimisticLocking` (2 updates concorrentes com mesma `version` → segundo recebe `ErrConcurrentUpdate`) — deve falhar
- [ ] 🟢 Implementar `UpdateAfterCollect` com cláusula `WHERE version = ?` e checagem de `rowsAffected` — mínimo para passar
- [ ] 🔵 Refatorar: extrair helper `setupTestDB(t)` se houver duplicação entre testes; revisar nomes; rodar `golangci-lint run`

**Arquivo(s) de teste a criar:**
- `internal/modules/scraping/sources/domain/source_test.go`
- `internal/modules/scraping/sources/infrastructure/pg_repository_test.go`

**Comandos a rodar:**
- Migrations: `migrate -path db/migrations -database "$DATABASE_URL" up`
- sqlc: `sqlc generate`
- Testes: `go test ./... -race -cover`
- Lint: `golangci-lint run`
- Format: `gofmt -s -w . && goimports -w .`

## 🔄 Plano de rollback

- **Feature flag:** N/A — worker ainda não existe, mudança não atinge produção
- **Reversibilidade de migration:** Reversível via `db/migrations/000001_init_stores_sources.down.sql` (`DROP TABLE sources; DROP TABLE stores;`)
- **Passos de rollback:** `migrate -path db/migrations -database "$DATABASE_URL" down 1`; remover diretórios `internal/modules/scraping/sources/` e `db/`; `git revert` do commit
- **Raio de explosão:** zero em produção. Em ambiente local apenas, os artefatos são removíveis sem efeito colateral em outros sistemas

## ✅ DoD (Definition of Done)

**Qualidade de código**
- [ ] Implementado conforme descrição técnica acima
- [ ] Segue convenções do `CLAUDE.md` (snake-lowercase para pacote, snake_case para arquivo, PascalCase para tipos exportados)
- [ ] Sem `panic`, sem `fmt.Println` em código de produção, sem `time.Now()` direto em `domain/` (Clock injetado quando necessário)
- [ ] Sem números mágicos; constantes nomeadas (ex.: `defaultEligibleLimit = 100`)

**TDD e testes**
- [ ] Testes escritos antes ou junto da implementação, não depois
- [ ] Testes moram **neste** Passo
- [ ] Todos os testes novos passando: `go test ./... -race -cover`
- [ ] Nenhum teste existente quebrou
- [ ] Caminho feliz + bordas (snapshot nil, version conflict, fonte desabilitada, dentro do intervalo) + cenários de erro cobertos

**Segurança**
- [ ] Input sanitizado onde aplicável (validação de URL via `net/url.Parse`, validação de `strategy` via CHECK constraint no DB)
- [ ] Autorização verificada — toda query filtra por `org_id` (multi-tenant, regra global do `CLAUDE.md`)
- [ ] Dados sensíveis não logados — N/A nesta capacidade (sem PII)
- [ ] Nenhum segredo hardcoded; `DATABASE_URL` lida via env; nenhum SQL injection (queries 100% via sqlc, sem concatenação)

**Performance**
- [ ] Sem N+1 — `GetEligibleSources` é uma query única com `LIMIT`
- [ ] Índice `(enabled, last_collected_at)` criado na migration; confirmar com `EXPLAIN` que o scheduler usa esse índice
- [ ] Sem operação síncrona pesada — todas as operações são single-statement no pool
- [ ] ✓ N/A — front-end não aplica
- [ ] Cache: N/A nesta camada — pgxpool já gerencia conexões

**Rate limit & throttling**
- [ ] ✓ N/A — este Passo é só camada de persistência; rate limit das lojas-alvo é do Passo 03
- [ ] ✓ N/A — não há endpoints públicos neste Passo
- [ ] ✓ N/A — sem cliente front

**Concorrência & idempotência**
- [ ] `UpdateSourceAfterCollect` usa optimistic locking via `version` (concorrência protegida)
- [ ] `WithTx` permite compor transação no use case (Passo 02 vai consumir)
- [ ] ✓ N/A — Idempotency-Key é para endpoints; aqui não há endpoint
- [ ] ✓ N/A — front não aplica

**Testes de regressão**
- [ ] Casos críticos (version conflict, fonte desabilitada não aparece em GetEligible) têm teste explícito por intenção
- [ ] N/A — sem bug fix neste Passo
- [ ] N/A — sem mudança em endpoint
- [ ] N/A — sem snapshot test

**Revisão e merge**
- [ ] PR aberto com descrição linkando o Cenário pai (`detectar-queda-de-preco-em-fonte-http`)
- [ ] Descrição do PR explica *o quê* e *por quê*, não *como*
- [ ] Code review aprovado (ver `CLAUDE.md`)
- [ ] CI verde (`go test ./... -race -cover`, `golangci-lint run`, `govulncheck ./...`)

## 🔗 Referências

- **Padrão de código a seguir:** seções "Estrutura" e "Convenções de nomenclatura" do `CLAUDE.md`
- **Docs relevantes:** [sqlc docs](https://docs.sqlc.dev/), [golang-migrate](https://github.com/golang-migrate/migrate), [shopspring/decimal](https://github.com/shopspring/decimal)
- **ADRs relacionados:** decisões 1, 2, 6 do `DESENHO.md` (scheduling via river, `last_snapshot` como `jsonb` na própria tabela, idempotência via unique constraint)
