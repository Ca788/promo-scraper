---
artefato: passo
cenario_local: ../cenarios/detectar-queda-de-preco-em-fonte-http/CENARIO.md
numero: 02
fase: Módulo `promo_events` + use case `CollectSource`
depende_de: [1]
criado_em: 2026-06-28T17:00:36Z
---

# Passo 02 — Módulo `promo_events` + use case `CollectSource`

> **Contexto do projeto:** veja `CLAUDE.md` na raiz do repositório.
>
> **Para o agente de execução:** este Passo é uma **fatia vertical** — agrupa migration de `promo_events`, queries sqlc, domínio do evento, repositório PG, e o use case `CollectSource` que orquestra tudo. A interface `Collector` é definida aqui (sem implementação real, que vem no Passo 03); o use case é testado com um fake `Collector` cobrindo os 4 critérios BDD do Cenário pai.
>
> **TDD é obrigatório:** os testes do use case são a tradução direta dos critérios do `CENARIO.md`. Escreva-os primeiro.
>
> **Leia nesta ordem antes de escrever código:**
> 1. O `CENARIO.md` pai (`../cenarios/detectar-queda-de-preco-em-fonte-http/CENARIO.md`) — os 4 critérios BDD viram 4 testes do use case
> 2. A `ESPEC.md` avó — restrição de idempotência
> 3. O `DESENHO.md` — schema canônico de `promo_events` (seção "Interfaces e contratos chave")
> 4. Os arquivos do Passo 01 (`Source`, `Snapshot`, `SourceRepository`) — o use case consome essa interface
> 5. O `CLAUDE.md` da raiz

**Fase:** Módulo `promo_events` + use case `CollectSource`
**Depende de:** 1
**Atrás de feature flag:** Não — worker ainda não existe em produção

## 📋 Descrição técnica

**Objetivo deste passo:** entregar a coluna vertebral lógica da capacidade. Criar persistência idempotente para o evento `promo detectada`, definir o use case `CollectSource` que orquestra fetch + comparação + materialização + atualização da fonte, e a interface `Collector` (abstração da camada de rede, implementada no Passo 03). O use case é o **lugar onde os 4 critérios BDD do Cenário se materializam em testes**.

**Comportamento atual:** após o Passo 01, `sources` e `stores` existem; não há tabela `promo_events`, não há use case, não há interface `Collector`.

**Comportamento esperado após este passo:**
- Migration 000002 cria `promo_events` particionada por mês em `detected_at`, com coluna gerada `dedup_bucket = date_trunc('hour', detected_at) + interval '30 min' * (extract(minute from detected_at)::int / 30)` STORED e UNIQUE `(org_id, source_id, preco, dedup_bucket)`, mais índices `(org_id, detected_at DESC)` e `(org_id, source_id, detected_at DESC)`.
- `PromoEventRepository.Insert` usa `INSERT ... ON CONFLICT DO NOTHING` no índice único, retornando `(inserted bool, err error)` para distinguir "novo evento" de "duplicata silenciada".
- Interface `Collector` exposta em `application/collector.go`: `Collect(ctx, src Source) (Snapshot, error)`; erros tipados `ErrSelectorNotMatched`, `ErrInvalidPrice`, `ErrFetchFailed`.
- Use case `CollectSource.Execute(ctx, input)` aplica o fluxo principal do Cenário:
  1. Buscar `Source` por ID via `SourceRepository.GetByID`.
  2. Chamar `Collector.Collect(ctx, source)`.
  3. Se retornar `ErrSelectorNotMatched` ou `ErrInvalidPrice`: chamar `MarkError`, **não** materializar evento, **não** atualizar snapshot, retornar `nil` (não-retentável).
  4. Se retornar `ErrFetchFailed`: propagar erro (será retentado pelo river).
  5. Comparar snapshot retornado com `source.Snapshot` via `HasPriceDrop`. Se queda detectada, inserir `PromoEvent` (ignorando conflito por unique).
  6. Em qualquer caminho de sucesso de fetch, atualizar `source.Snapshot` via `UpdateAfterCollect`.
- Use case usa `Clock` injetado (sem `time.Now()` direto, conforme CLAUDE.md).
- Suite de testes verde, incluindo testes que **espelham diretamente** os 4 critérios BDD do `CENARIO.md` usando um fake `Collector` (e fakes ou repositórios reais para PG, decidir caso a caso).

## 📁 Arquivos a criar ou modificar

| Ação | Caminho do arquivo | O que fazer |
|---|---|---|
| Criar | `db/migrations/000002_init_promo_events.up.sql` | `CREATE TABLE promo_events (...)` exatamente como no `DESENHO.md`, com `PARTITION BY RANGE (detected_at)` + partição inicial do mês corrente e do mês seguinte; coluna gerada `dedup_bucket` STORED; UNIQUE `(org_id, source_id, preco, dedup_bucket)`; índices `(org_id, detected_at DESC)` e `(org_id, source_id, detected_at DESC)` |
| Criar | `db/migrations/000002_init_promo_events.down.sql` | `DROP TABLE promo_events;` |
| Criar | `db/queries/promo_events.sql` | `-- name: InsertPromoEvent :execrows` com `ON CONFLICT (org_id, source_id, preco, dedup_bucket) DO NOTHING`, retornando linhas afetadas (0 = duplicata, 1 = inserido) |
| Criar | `internal/modules/scraping/promo_events/domain/promo_event.go` | Struct `PromoEvent` (todos os campos da tabela exceto `dedup_bucket` que é gerado); factory `NewPromoEvent(source Source, snap Snapshot, clock Clock) PromoEvent` que preenche `PrecoAnterior` a partir de `source.Snapshot.Preco` (ou nil quando primeiro snapshot), `DetectedAt = clock.Now()`, demais campos copiados |
| Criar | `internal/modules/scraping/promo_events/domain/repository.go` | Interface `PromoEventRepository { Insert(ctx, e PromoEvent) (inserted bool, err error) }` |
| Criar | `internal/modules/scraping/promo_events/domain/promo_event_test.go` | Unit: `TestNewPromoEvent_FillsPrecoAnterior` (source com snapshot anterior → `PrecoAnterior` populado); `TestNewPromoEvent_NilSnapshot_NoPrecoAnterior` (primeiro poll → `PrecoAnterior` nil); `TestNewPromoEvent_UsesInjectedClock` (`DetectedAt` igual ao Clock fake) |
| Criar | `internal/modules/scraping/promo_events/infrastructure/pg_repository.go` | Impl `PGPromoEventRepository` consumindo sqlc; em `Insert`, mapeia `rowsAffected` para `inserted bool` |
| Criar | `internal/modules/scraping/promo_events/infrastructure/pg_repository_test.go` | Integração contra Postgres: `TestInsert_NewEvent_ReturnsInsertedTrue`; `TestInsert_SameBucket_ReturnsInsertedFalse` (insere 2 vezes mesmo `(source_id, preco)` dentro de 30min → 2ª retorna `inserted=false`); `TestInsert_DifferentBucket_ReturnsInsertedTrue` (manipula `detected_at` para janela seguinte) |
| Criar | `internal/shared/clock/clock.go` | Interface `Clock { Now() time.Time }` + `SystemClock{}` + `FakeClock{T time.Time; func (f *FakeClock) Now() time.Time { return f.T }}` — pacote compartilhado pelo restante do projeto |
| Criar | `internal/modules/scraping/collection/application/collector.go` | `package application`; `var ErrSelectorNotMatched = errors.New(...); var ErrInvalidPrice = errors.New(...); var ErrFetchFailed = errors.New(...)`; `type Collector interface { Collect(ctx, src domain.Source) (sources.Snapshot, error) }` |
| Criar | `internal/modules/scraping/collection/application/collect_source.go` | Struct `CollectSourceUseCase { sources SourceRepository; events PromoEventRepository; collector Collector; clock Clock; logger *slog.Logger }`; método `Execute(ctx, input{OrgID, SourceID, ExpectedVersion}) error` implementando o fluxo descrito acima; observa: em `ErrSelectorNotMatched`/`ErrInvalidPrice` chama `MarkError` e retorna `nil`; em `ErrFetchFailed` retorna o erro para o caller (river retenta) |
| Criar | `internal/modules/scraping/collection/application/collect_source_test.go` | Unit do use case com fakes (`FakeCollector`, `InMemorySourceRepo`, `InMemoryPromoEventRepo`, `FakeClock`): 1 teste por critério BDD do `CENARIO.md` — caminho feliz / seletor não casa / preço zero/negativo / timeout 5xx |

## 📚 Contexto a ler antes

| Caminho do arquivo | Por que importa |
|---|---|
| `CLAUDE.md` | Convenções; em particular "Use Case = struct + `Execute(ctx, input) (output, error)`. Sem `Service` faz-tudo" e "`time.Now()` em domain — injete `Clock`" |
| `.spec/especificacoes/coleta-de-promo/CENARIO.md` (via path acima) | 4 critérios BDD → 4 testes do use case |
| `.spec/especificacoes/coleta-de-promo/DESENHO.md` | Schema canônico de `promo_events`, regra de `dedup_bucket`, decisão de detecção (queda ≥ 1 centavo) |
| Arquivos do Passo 01 (`source.go`, `repository.go`, `pg_repository.go`) | Contratos consumidos pelo use case |

## 🧪 Plano TDD

**Ciclo Vermelho/Verde/Refatorar:**

- [ ] 🔴 Escrever `TestCollectSource_HappyPath` (fake Collector retorna snapshot com `preco=90,00`; fonte com `last_snapshot.Preco=100,00`) → asserções: 1 chamada a `events.Insert` com `Preco=90,00` e `PrecoAnterior=100,00`; 1 chamada a `sources.UpdateAfterCollect` com snapshot atualizado; retorna `nil` — deve falhar (use case não existe)
- [ ] 🟢 Implementar fluxo principal do `CollectSourceUseCase.Execute` — mínimo para passar
- [ ] 🔴 Escrever `TestCollectSource_SelectorNotMatched` (Collector retorna `ErrSelectorNotMatched`) → 0 chamadas a `events.Insert`; 0 chamadas a `UpdateAfterCollect`; 1 chamada a `MarkError` com mensagem identificando seletor; retorna `nil` (não-retentável); **mesmo teste cobre preço zero** com `ErrInvalidPrice` — usar tabela
- [ ] 🟢 Implementar branching de erro com `MarkError` e early-return — mínimo para passar
- [ ] 🔴 Escrever `TestCollectSource_FetchFailed_PropagatesError` (Collector retorna `ErrFetchFailed`) → 0 chamadas a `events.Insert`, 0 chamadas a `UpdateAfterCollect`, 0 chamadas a `MarkError` (river vai retentar); retorna o erro envelopado com `%w` — deve falhar
- [ ] 🟢 Implementar propagação de `ErrFetchFailed` — mínimo para passar
- [ ] 🔵 Refatorar: extrair fakes para `testdata/fakes.go` ou arquivo helper; revisar logs estruturados (slog com `source_id`, `org_id`, `result`)

**Arquivo(s) de teste a criar:**
- `internal/modules/scraping/promo_events/domain/promo_event_test.go`
- `internal/modules/scraping/promo_events/infrastructure/pg_repository_test.go`
- `internal/modules/scraping/collection/application/collect_source_test.go`

**Comandos a rodar:**
- Migrations: `migrate -path db/migrations -database "$DATABASE_URL" up`
- sqlc: `sqlc generate`
- Testes: `go test ./... -race -cover`
- Lint: `golangci-lint run`

## 🔄 Plano de rollback

- **Feature flag:** N/A — worker não existe em produção
- **Reversibilidade de migration:** Reversível via `db/migrations/000002_init_promo_events.down.sql`
- **Passos de rollback:** `migrate down 1`; reverter commit (`git revert`); regenerar sqlc se necessário
- **Raio de explosão:** zero em produção

## ✅ DoD (Definition of Done)

**Qualidade de código**
- [ ] Implementado conforme descrição técnica acima
- [ ] Segue convenções do `CLAUDE.md` (handler thin não se aplica aqui; mas estrutura de Use Case sim)
- [ ] Sem `panic`; sem `time.Now()` em `application/` ou `domain/` (use `Clock` injetado)
- [ ] Sem números mágicos; janela de dedup expressa em SQL (não hardcoded em Go)

**TDD e testes**
- [ ] Testes escritos antes da implementação
- [ ] Testes moram **neste** Passo
- [ ] Todos os testes novos passando: `go test ./... -race -cover`
- [ ] Nenhum teste existente quebrou (incluindo os do Passo 01)
- [ ] **Os 4 critérios BDD do `CENARIO.md` têm teste 1:1 em `collect_source_test.go`**

**Segurança**
- [ ] Toda query filtra `org_id` (multi-tenant)
- [ ] Dados sensíveis não logados (preço e SKU não são sensíveis; sem PII envolvido)
- [ ] Sem segredo hardcoded; SQL 100% via sqlc

**Performance**
- [ ] Particionamento por mês ativo desde o início (evita reparticionamento caro depois)
- [ ] Índices `(org_id, detected_at DESC)` e `(org_id, source_id, detected_at DESC)` criados — confirmar com `EXPLAIN` em prod-like
- [ ] `INSERT ... ON CONFLICT DO NOTHING` é single-statement, sem leitura prévia (idempotência sem race)
- [ ] ✓ N/A — frontend não aplica

**Rate limit & throttling**
- [ ] ✓ N/A — sem endpoint público; rate-limit das lojas é do Passo 03

**Concorrência & idempotência**
- [ ] `UNIQUE (org_id, source_id, preco, dedup_bucket)` garante idempotência no nível do banco
- [ ] Use case usa `WithTx` quando precisa atomicidade entre `Insert event` + `UpdateAfterCollect` (mesmo se um falhar, ambos revertem)
- [ ] ✓ N/A — sem endpoint para `Idempotency-Key`

**Testes de regressão**
- [ ] Caso de dedup no mesmo bucket coberto (`TestInsert_SameBucket_ReturnsInsertedFalse`)
- [ ] Caso de optimistic lock conflict propagado pelo use case (cobrir indiretamente via `UpdateAfterCollect`)

**Revisão e merge**
- [ ] PR linkando Cenário + Passo 01 como dependência
- [ ] CI verde

## 🔗 Referências

- **Padrão de código a seguir:** "Use Case = struct + `Execute(ctx, input) (output, error)`" do `CLAUDE.md`
- **Docs relevantes:** [Postgres partitioning](https://www.postgresql.org/docs/16/ddl-partitioning.html), [INSERT ON CONFLICT](https://www.postgresql.org/docs/16/sql-insert.html#SQL-ON-CONFLICT)
- **ADRs relacionados:** decisões 3, 4, 7 do `DESENHO.md` (idempotência via unique constraint, detecção de variação por queda ≥ 1 centavo, particionamento por mês)
