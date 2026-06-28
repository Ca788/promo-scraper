---
artefato: passo
cenario_local: ../cenarios/detectar-queda-de-preco-em-fonte-http/CENARIO.md
numero: 04
fase: Worker `river` + observabilidade + bootstrap final
depende_de: [3]
criado_em: 2026-06-28T17:00:36Z
---

# Passo 04 — Worker `river` + observabilidade + bootstrap final

> **Contexto do projeto:** veja `CLAUDE.md` na raiz do repositório.
>
> **Para o agente de execução:** este Passo é o **fechamento da fatia vertical** do Cenário. Liga as peças (use case + collector + repositórios) num processo executável: `cmd/worker` com `river` consumindo jobs de coleta enfileirados por um scheduler periódico, métricas Prometheus expostas em `/metrics`, e teste end-to-end automatizado (a versão do roteiro manual do Cenário).
>
> **TDD:** o teste end-to-end deste Passo é o **selo do Cenário**. Se ele passa, o Cenário inteiro está cumprido. Escreva-o primeiro.
>
> **Leia nesta ordem antes de escrever código:**
> 1. O `CENARIO.md` pai — em especial a seção "🧪 Roteiro de validação manual": os passos 1-10 viram o teste end-to-end deste Passo
> 2. O `DESENHO.md` — seção "Considerações de dados" (volume esperado, instrumentação)
> 3. Os arquivos dos Passos 01, 02, 03 — todos consumidos aqui
> 4. O `CLAUDE.md` — em particular "Logger sem `request_id` + `org_id`" é proibição, e "envelope JSON canônico" não se aplica a worker (apenas para HTTP), mas slog estruturado é obrigatório

**Fase:** Worker `river` + observabilidade + bootstrap final
**Depende de:** 3
**Atrás de feature flag:** Não — primeira execução em qualquer ambiente; o controle aqui é via env `WORKER_ENABLED` (default `true` em dev, configurável em prod). Não é feature flag, é kill switch operacional

## 📋 Descrição técnica

**Objetivo deste passo:** entregar um processo `cmd/worker` executável que (a) lê configuração validada na startup, (b) abre `pgxpool` para Postgres, (c) configura `river` com o driver pgxv5, (d) registra o worker `CollectSourceJob`, (e) inicia um scheduler periódico que lê fontes elegíveis e enfileira jobs, (f) expõe `/metrics` Prometheus, (g) loga em JSON via `slog`.

**Comportamento atual:** após o Passo 03, todas as peças existem em `internal/`, mas nenhum processo as executa. O `main.go` ainda é "hello world".

**Comportamento esperado após este passo:**
- `go run ./cmd/worker` sobe, valida config (falha rápido se inválida), conecta no Postgres, registra worker, inicia scheduler de 5s e servidor HTTP de métricas na porta `PORT_METRICS` (default `:9090`).
- A cada tick do scheduler, lê `GetEligibleSources(orgID, limit=100)` por org (configurável quais orgs servir) e enfileira `CollectSourceJob{OrgID, SourceID, Version}` no river. Sem duplicação: usa `river.UniqueOpts{ByArgs: true}` para evitar 2 enqueues do mesmo job em transit.
- O job handler chama `CollectSourceUseCase.Execute`. Em sucesso (incluindo o caso `ErrSelectorNotMatched`/`ErrInvalidPrice` tratado pelo use case), retorna `nil`. Em `ErrFetchFailed`, retorna o erro — `river` aplica backoff exponencial + jitter (config default: 5 tentativas).
- Histograma `collection_duration_seconds{store, strategy, result}` registra cada execução; counter `collection_errors_total{store, kind}` incrementa nos casos de erro.
- Teste end-to-end: spawn de Postgres + `httptest.Server` simulando loja; insere fonte de teste com snapshot inicial; ativa river inline (`riverpgxv5.New` com `JobInsertMiddleware` em modo síncrono); dispara um tick; espera o job completar; verifica 1 linha em `promo_events`, snapshot atualizado, métrica registrada.

## 📁 Arquivos a criar ou modificar

| Ação | Caminho do arquivo | O que fazer |
|---|---|---|
| Criar | `internal/config/config.go` | `type Config struct { DatabaseURL string; PortMetrics string; SchedulerInterval time.Duration; WorkerOrgIDs []uuid.UUID; LogLevel slog.Level }`; `func Load() (Config, error)` lê env via `os.Getenv` e valida (DatabaseURL obrigatória, parseia outras); usa `go-playground/validator` se quiser (ou validação manual mais leve para começar) |
| Criar | `internal/config/config_test.go` | `TestLoad_FailsWhenDatabaseURLMissing`; `TestLoad_AppliesDefaults`; `TestLoad_ParsesSchedulerInterval` |
| Criar | `internal/shared/observability/metrics.go` | Registry Prometheus global; histograma `collection_duration_seconds` (labels `store_id`, `strategy`, `result` com buckets de 0.05s a 30s); counter `collection_errors_total` (labels `store_id`, `kind` ∈ {`fetch_failed`, `selector_not_matched`, `invalid_price`, `concurrent_update`, `internal`}) |
| Criar | `internal/modules/scraping/collection/infrastructure/river_job.go` | `type CollectSourceArgs struct { OrgID uuid.UUID; SourceID int64; Version int }` com `func (CollectSourceArgs) Kind() string { return "collect_source" }`; `type CollectSourceWorker struct { river.WorkerDefaults[CollectSourceArgs]; uc *application.CollectSourceUseCase; metrics *observability.Metrics }`; método `Work(ctx, *river.Job[CollectSourceArgs]) error` que invoca `uc.Execute` e mede latência registrando no histograma com `result` ∈ {`success`, `error`} |
| Criar | `internal/modules/scraping/collection/infrastructure/scheduler.go` | `type Scheduler struct { sources sources.SourceRepository; client *river.Client[pgx.Tx]; interval time.Duration; orgIDs []uuid.UUID; logger *slog.Logger }`; método `Run(ctx) error` com loop `time.NewTicker(s.interval)`; a cada tick, para cada `orgID`, chama `sources.GetEligible(ctx, orgID, 100)` e enfileira jobs (`client.Insert(ctx, CollectSourceArgs{...}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 30*time.Second}})`); para no ctx.Done() |
| Criar | `internal/modules/scraping/collection/infrastructure/scheduler_test.go` | Integração curta: insere 2 fontes elegíveis + 1 não-elegível; chama `Scheduler.runOnce(ctx)` (refatorar `Run` para extrair `runOnce`); confere que 2 jobs foram enfileirados (consultar river inline) |
| Criar | `internal/modules/scraping/collection/infrastructure/river_job_test.go` | **Teste end-to-end do Cenário** (selo). Setup: Postgres real (docker-compose) + `httptest.Server` servindo HTML do produto com `preco=90,00` (estado inicial da fonte é `last_snapshot.preco=100,00`) + river inline + use case montado com todas as deps reais. Casos: (a) caminho feliz → 1 linha em `promo_events`, snapshot atualizado, métrica `collection_duration_seconds{result="success"}` registrada; (b) servidor responde 500 → após retries esgotados (configurar `MaxAttempts: 2` no teste para acelerar), `last_error` populado, sem evento, counter `collection_errors_total{kind="fetch_failed"}` incrementado; (c) servidor responde HTML sem seletor → `last_error` populado, snapshot inalterado |
| Criar | `cmd/worker/main.go` | `func main()`: 1) `slog.New(slog.NewJSONHandler(...))`; 2) `cfg, err := config.Load()` → `os.Exit(1)` em erro com log estruturado; 3) `pool, err := pgxpool.New(ctx, cfg.DatabaseURL)`; 4) `riverClient, err := river.NewClient(riverpgxv5.New(pool), riverConfig)` com `Workers` registrando `CollectSourceWorker`; 5) `riverClient.Start(ctx)`; 6) instanciar repositórios, use case, collector, scheduler; 7) `go scheduler.Run(ctx)`; 8) `go serveMetrics(ctx, cfg.PortMetrics)`; 9) trap SIGTERM/SIGINT, `riverClient.Stop(ctx)`, fecha pool; 10) graceful shutdown com timeout |
| Modificar | `main.go` (raiz) | Remover "hello world"; deixar arquivo curto delegando para `cmd/worker` ou apagar (manter `cmd/worker/main.go` como entrypoint principal) — preferir **deletar** o `main.go` raiz, já que `cmd/worker/main.go` assume |
| Deletar | `main.go` | Substituído por `cmd/worker/main.go` |
| Modificar | `go.mod` / `go.sum` | Adicionar `github.com/riverqueue/river`, `github.com/riverqueue/river/riverdriver/riverpgxv5`, `github.com/prometheus/client_golang/prometheus`, `github.com/prometheus/client_golang/prometheus/promhttp`, `github.com/google/uuid` (se ainda não estiver) |
| Criar | `cmd/worker/README.md` | Como rodar local: `docker compose up -d postgres`; `migrate -path db/migrations -database "$DATABASE_URL" up`; `go run github.com/riverqueue/river/cmd/river migrate-up --database-url "$DATABASE_URL"`; `go run ./cmd/worker` |

> **Nota sobre `river migrate-up`:** o river requer suas próprias tabelas (`river_job`, `river_leader`, etc.) — comando separado das migrations da aplicação. Documentar no README.

## 📚 Contexto a ler antes

| Caminho do arquivo | Por que importa |
|---|---|
| `CLAUDE.md` | Estrutura `cmd/worker`, `internal/shared/...`, proibição `Logger sem request_id + org_id` (worker usa `org_id` como contexto), comando `go test ./... -race -cover` |
| `CENARIO.md` (pai) | Roteiro manual de validação (10 passos) → traduzir em teste end-to-end |
| `DESENHO.md` | Seções "Considerações de dados" (volume esperado, para dimensionar pool e burst do bucket) e "Riscos e mitigações" (alarme em queue depth — relevante para o que instrumentar) |
| Arquivos dos Passos 01, 02, 03 | Todos os contratos consumidos pelo worker |
| `docker-compose.yml` | Confirma `DATABASE_URL` local para o README |

## 🧪 Plano TDD

**Ciclo Vermelho/Verde/Refatorar:**

- [ ] 🔴 Escrever `TestConfigLoad_Failures` (DATABASE_URL ausente → erro com mensagem clara; PORT_METRICS inválido → erro) — deve falhar
- [ ] 🟢 Implementar `config.Load` com validações — mínimo para passar
- [ ] 🔴 Escrever `TestScheduler_EnqueuesEligibleSources` (insere 2 elegíveis + 1 não → `runOnce` enfileira exatamente 2 jobs; consultar tabela `river_job`) — deve falhar
- [ ] 🟢 Implementar `Scheduler.runOnce` consumindo `GetEligible` + `river.Insert` — mínimo para passar
- [ ] 🔴 Escrever `TestEndToEnd_HappyPath_CollectsAndMaterializesEvent` (httptest serve HTML válido com queda → após o job, 1 linha em `promo_events`, snapshot atualizado, histograma registrou amostra) — deve falhar
- [ ] 🟢 Implementar `CollectSourceWorker.Work` + bootstrap em `cmd/worker/main.go` — mínimo para passar
- [ ] 🔴 Escrever `TestEndToEnd_FetchFailed_RetriesThenMarksError` (httptest responde 500 sempre; configurar `MaxAttempts: 2`; após 2 tentativas falhadas, `sources.last_error` populado, sem evento, counter `collection_errors_total{kind="fetch_failed"}` incrementado) — deve falhar
- [ ] 🟢 Garantir que o worker propaga `ErrFetchFailed` corretamente, deixando o river tratar; ao final dos retries, o use case chama `MarkError` (decisão: marca após esgotamento, não dentro de cada tentativa — implementar via hook `JobInsertMiddleware` ou método separado chamado quando `Job.Attempt == MaxAttempts`) — mínimo para passar
- [ ] 🔵 Refatorar: extrair `setupTestWorker(t)` para reuso entre testes end-to-end; revisar logs do `Work` (`source_id`, `org_id`, `attempt`, `duration_ms`, `result`); confirmar que cada linha de log inclui `org_id`

**Arquivo(s) de teste a criar:**
- `internal/config/config_test.go`
- `internal/modules/scraping/collection/infrastructure/scheduler_test.go`
- `internal/modules/scraping/collection/infrastructure/river_job_test.go`

**Comandos a rodar:**
- Adicionar deps: `go get github.com/riverqueue/river github.com/riverqueue/river/riverdriver/riverpgxv5 github.com/prometheus/client_golang/prometheus`
- Migrations app: `migrate -path db/migrations -database "$DATABASE_URL" up`
- Migrations river: `go run github.com/riverqueue/river/cmd/river migrate-up --database-url "$DATABASE_URL"`
- Testes: `go test ./... -race -cover`
- Rodar worker: `go run ./cmd/worker`
- Suite full + lint: `go test ./... -race -cover && golangci-lint run && govulncheck ./...`

## 🔄 Plano de rollback

- **Feature flag:** N/A — primeiro worker do projeto, sem comportamento anterior em produção
- **Kill switch operacional:** env `WORKER_ENABLED=false` faz o `main.go` apenas inicializar métricas e dormir (não processar jobs) — útil para parar processamento sem killar o processo
- **Reversibilidade de migrations:** ✓ N/A — este Passo não adiciona migrations da aplicação; as migrations do river são reversíveis via `river migrate-down`
- **Passos de rollback:** kill do processo do worker (SIGTERM); jobs em transit no river continuam pendentes no banco e podem ser consumidos por uma versão anterior do worker quando reiniciado; se necessário descartar fila, `DELETE FROM river_job WHERE state IN ('available','running')` em janela controlada
- **Raio de explosão:** worker offline = nenhum evento `promo detectada` materializado durante o downtime. Como a Espec não promete SLA de cobertura de promo (só latência quando coletada), o impacto é "perda silenciosa de oportunidades de alerta" — aceitável para downtime curto, monitorar via alarme em ausência de eventos por janela > X minutos

## ✅ DoD (Definition of Done)

**Qualidade de código**
- [ ] Implementado conforme descrição técnica acima
- [ ] Segue convenções do `CLAUDE.md` (`cmd/worker/main.go` curto, lógica em `internal/modules/...`)
- [ ] Sem `panic`; erro de bootstrap loga e `os.Exit(1)` (aceitável em main); errors em runtime sempre retornados/logados estruturado
- [ ] Sem `fmt.Println`; tudo via `slog`
- [ ] Sem número mágico: timeout, interval, burst, MaxAttempts — constantes/config nomeadas

**TDD e testes**
- [ ] Testes escritos antes da implementação
- [ ] Testes moram **neste** Passo
- [ ] Todos os testes passando: `go test ./... -race -cover`
- [ ] Nenhum teste existente quebrou (Passos 01, 02, 03)
- [ ] **Teste end-to-end** cobre os 4 critérios BDD do Cenário pai num único processo real (Postgres + river + httptest)

**Segurança**
- [ ] `DATABASE_URL` lida via env, nunca hardcoded; nenhum segredo em log
- [ ] `slog` configurado para NÃO incluir o conteúdo de `selectors` em logs de erro (pode conter caminhos sensíveis no futuro)
- [ ] `/metrics` exposto sem autenticação na porta separada (`PORT_METRICS`); em produção, esse port só deve ser acessível pela rede interna (documentar no README)
- [ ] CORS / origens cruzadas: N/A — endpoint `/metrics` não é browser-facing
- [ ] Sem PII

**Performance**
- [ ] `pgxpool` configurado com `MaxConns` adequado (sugestão: 10–20 para começar; revisar com `EXPLAIN` se necessário)
- [ ] Histograma `collection_duration_seconds` com buckets cobrindo de 50ms a 30s (relevante para o p95 < 60s da métrica do norte)
- [ ] Scheduler usa `GetEligibleSources` com `LIMIT 100` por tick por org — evita avalanche
- [ ] river `UniqueOpts.ByPeriod` evita re-enqueue do mesmo job em janela curta
- [ ] ✓ N/A — frontend não aplica

**Rate limit & throttling**
- [ ] Worker respeita `TokenBucket` do Passo 03 (não precisa adicionar nada aqui)
- [ ] `/metrics` não tem rate limit (típico para scrape de Prometheus interno) — documentar
- [ ] ✓ N/A — sem cliente front

**Concorrência & idempotência**
- [ ] river garante "ao menos uma execução" de cada job; uso da unique constraint do Passo 02 absorve duplicação eventual
- [ ] `UniqueOpts.ByArgs` no enqueue evita 2 jobs em transit para a mesma fonte
- [ ] Scheduler é single-leader via `river.Client` (river já lida com isso em deployments multi-réplica)
- [ ] ✓ N/A — sem endpoint para `Idempotency-Key`

**Testes de regressão**
- [ ] Teste end-to-end com mock 500 cobre o caso "loja indisponível" — sinal forte de regressão futura se quebrar
- [ ] Teste end-to-end com seletor não casa cobre o risco mais provável de regressão (lojas mudam HTML)
- [ ] Métricas verificadas em teste para evitar regressão de instrumentação silenciosa

**Revisão e merge**
- [ ] PR linkando Cenário + Passos 01–03 como dependências
- [ ] Descrição do PR explica que após o merge, `go run ./cmd/worker` é executável
- [ ] CI verde: `go test ./... -race -cover`, `golangci-lint run`, `govulncheck ./...`

## 🔗 Referências

- **Padrão de código a seguir:** `cmd/worker/main.go` (estrutura) do `CLAUDE.md`
- **Docs relevantes:** [river docs](https://riverqueue.com/docs), [prometheus client_golang](https://github.com/prometheus/client_golang), [slog handler](https://pkg.go.dev/log/slog#JSONHandler)
- **ADRs relacionados:** decisão 2 do `DESENHO.md` (river via Postgres, sem Redis)
