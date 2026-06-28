---
artefato: passo
cenario_local: ../cenarios/detectar-queda-de-preco-em-fonte-http/CENARIO.md
numero: 03
fase: Coletor HTTP real + parser + rate-limit
depende_de: [2]
criado_em: 2026-06-28T17:00:36Z
---

# Passo 03 — Coletor HTTP real + parser + rate-limit

> **Contexto do projeto:** veja `CLAUDE.md` na raiz do repositório.
>
> **Para o agente de execução:** este Passo implementa a interface `Collector` definida no Passo 02. A separação entre `HTTPCollector` (faz fetch + orquestra rate-limit + UA) e `Parser` (recebe HTML + selectors, devolve `Snapshot` ou erro tipado) é deliberada — permite testar o parser sem rede.
>
> **TDD:** os 4 critérios BDD do `CENARIO.md` são testados aqui via `httptest.Server` ponta-a-ponta. Esta é a primeira vez que cruzamos a fronteira de rede.
>
> **Leia nesta ordem antes de escrever código:**
> 1. O `CENARIO.md` pai — os 4 critérios viram 4 testes de integração com `httptest`
> 2. O `DESENHO.md` — seção "Abordagem escolhida" (decisão por net/http + goquery)
> 3. Os arquivos do Passo 02 (`collector.go` define a interface; `errors.go` define `ErrSelectorNotMatched`, `ErrInvalidPrice`, `ErrFetchFailed`)
> 4. O `CLAUDE.md` — convenções de erro como valores tipados, `context.Context` em toda função de IO

**Fase:** Coletor HTTP real + parser + rate-limit
**Depende de:** 2
**Atrás de feature flag:** Não — worker ainda não existe em produção

## 📋 Descrição técnica

**Objetivo deste passo:** implementação concreta da camada de rede. Cliente HTTP com timeout, User-Agent realista rotativo, token bucket por loja, parser que aplica seletores `goquery` para extrair `Snapshot` ou retornar erro tipado, e tradução de falhas de rede em erros tipados do domínio (`ErrFetchFailed`).

**Comportamento atual:** após o Passo 02, existe `Collector` como interface usada pelo use case, mas nenhuma implementação real — apenas fakes em teste.

**Comportamento esperado após este passo:**
- `HTTPCollector` implementa `Collector` para fontes com `strategy='http'`. Para `strategy='headless'`, retorna `ErrFetchFailed` com mensagem `"headless not implemented yet"` (fora de escopo deste cenário).
- `Parser.ParseProduct(html []byte, selectors map[string]string) (Snapshot, error)` extrai campos via `goquery`; normaliza preço brasileiro (`R$ 1.234,56` → `decimal.Decimal{1234.56}`); retorna `ErrSelectorNotMatched` quando o seletor de preço (`selectors["preco"]`) não casa; retorna `ErrInvalidPrice` quando preço extraído é `≤ 0`.
- `TokenBucketRegistry` mantém um `*rate.Limiter` por `store_id`, criando sob demanda; `Acquire(ctx, storeID)` bloqueia respeitando o limite (default: 30 req/min, configurável por store).
- Pool de User-Agents (slice constante com ~10 UAs Chrome/Firefox recentes); seleção pseudo-aleatória (`math/rand/v2`).
- Timeout default do `http.Client`: 10 segundos (configurável).
- Erros de rede traduzidos: `context.DeadlineExceeded` ou `net.Error.Timeout` → `ErrFetchFailed{Cause: timeout}`; status 5xx → `ErrFetchFailed{Cause: server error, StatusCode: X}`; status 4xx (exceto 404) → `ErrFetchFailed{Cause: client error}`.
- Suite verde via `go test ./... -race -cover`.

## 📁 Arquivos a criar ou modificar

| Ação | Caminho do arquivo | O que fazer |
|---|---|---|
| Criar | `internal/modules/scraping/collection/infrastructure/parser.go` | Função `ParseProduct(html []byte, selectors map[string]string) (sources.Snapshot, error)`; usa `goquery.NewDocumentFromReader(bytes.NewReader(html))`; aplica seletor de preço, título, SKU, estoque, badge; normaliza preço com `normalizeBRPrice(s string) (decimal.Decimal, error)` que trata "R$", "." como milhar, "," como decimal; retorna `ErrSelectorNotMatched` se preço não casar; `ErrInvalidPrice` se ≤ 0 |
| Criar | `internal/modules/scraping/collection/infrastructure/parser_test.go` | Tabela de casos: HTML completo válido → snapshot correto; HTML sem seletor de preço → `ErrSelectorNotMatched`; HTML com "R$ Esgotado" → `ErrSelectorNotMatched` (não casa formato numérico); preço "0,00" → `ErrInvalidPrice`; preço negativo (formato pouco usual mas testar) → `ErrInvalidPrice`; preço "1.234,56" → `decimal(1234.56)` |
| Criar | `internal/modules/scraping/collection/infrastructure/user_agents.go` | `var userAgents = []string{...}` (slice constante de ~10 UAs Chrome 130+/Firefox 132+); `func RandomUserAgent() string` retorna um aleatório usando `math/rand/v2` |
| Criar | `internal/modules/scraping/collection/infrastructure/token_bucket.go` | `type TokenBucketRegistry struct { mu sync.Mutex; buckets map[int64]*rate.Limiter; defaultRate rate.Limit; defaultBurst int }`; `New(...)`; `Acquire(ctx, storeID int64) error` — chama `limiter.Wait(ctx)`, criando o limiter sob demanda |
| Criar | `internal/modules/scraping/collection/infrastructure/token_bucket_test.go` | Unit: `TestAcquire_RespectsRate` (2 chamadas seguidas → 2ª aguarda); `TestAcquire_IsolatedPerStore` (loja A não bloqueia loja B); `TestAcquire_ContextCancelled` (cancela context → retorna erro de cancelamento) |
| Criar | `internal/modules/scraping/collection/infrastructure/http_collector.go` | `type HTTPCollector struct { client *http.Client; bucket *TokenBucketRegistry; logger *slog.Logger }`; `func New(timeout time.Duration, bucket *TokenBucketRegistry, logger) *HTTPCollector`; método `Collect(ctx, src Source) (Snapshot, error)`: 1) checa `src.Strategy == "http"` (senão `ErrFetchFailed{Cause: "unsupported strategy"}`); 2) `bucket.Acquire(ctx, src.StoreID)`; 3) cria `http.Request` com `Accept-Language: pt-BR`, `Accept-Encoding: gzip`, `User-Agent: RandomUserAgent()`; 4) executa fetch; 5) traduz erros (timeout, 5xx, 4xx) para `ErrFetchFailed`; 6) `body, _ := io.ReadAll`; 7) `ParseProduct(body, src.Selectors)` retorna direto |
| Criar | `internal/modules/scraping/collection/infrastructure/http_collector_test.go` | Integração com `httptest.NewServer`: `TestCollect_HappyPath_ReturnsSnapshot` (servidor responde HTML com preço 90,00 → snapshot extraído); `TestCollect_SelectorNotMatched` (HTML sem o seletor de preço → `ErrSelectorNotMatched`); `TestCollect_InvalidPrice` (HTML com preço 0,00 → `ErrInvalidPrice`); `TestCollect_ServerError5xx` (servidor responde 500 → `ErrFetchFailed`); `TestCollect_Timeout` (servidor demora além do timeout → `ErrFetchFailed`); `TestCollect_SendsRealisticUserAgent` (verifica que o request recebido tem UA do pool); `TestCollect_RespectsTokenBucket` (2 requests consecutivas, 2º aguarda) |
| Modificar | `go.mod` / `go.sum` | Adicionar `github.com/PuerkitoBio/goquery@latest`, `github.com/shopspring/decimal@latest` (se ainda não estiver), `golang.org/x/time@latest` (para `rate`) — via `go get` + `go mod tidy` |

## 📚 Contexto a ler antes

| Caminho do arquivo | Por que importa |
|---|---|
| `CLAUDE.md` | "Errors como valores tipados", "context.Context em toda função de IO" |
| `CENARIO.md` (pai) | 4 critérios BDD que viram 4 testes do `HTTPCollector` |
| `DESENHO.md` | Seção "Abordagem escolhida" (net/http + goquery; chromedp depois); seção "Riscos" (loja muda HTML/seletores — daí selectors em jsonb sobre `sources`) |
| Arquivos do Passo 02 (`collector.go`, erros tipados) | Interface a ser implementada e contratos de erro |

## 🧪 Plano TDD

**Ciclo Vermelho/Verde/Refatorar:**

- [ ] 🔴 Escrever `TestParseProduct_HappyPath` (tabela com 3+ HTMLs reais simplificados de Kabum/Pichau/Terabyte → snapshots esperados; `R$ 1.234,56`, `R$ 99,90`, `R$ 5.999,00`) — deve falhar
- [ ] 🟢 Implementar `ParseProduct` + `normalizeBRPrice` — mínimo para passar
- [ ] 🔴 Escrever `TestParseProduct_ErrorCases` (tabela: sem seletor de preço → `ErrSelectorNotMatched`; preço `"R$ Esgotado"` → `ErrSelectorNotMatched`; preço `"R$ 0,00"` → `ErrInvalidPrice`; preço negativo → `ErrInvalidPrice`) — deve falhar parcialmente
- [ ] 🟢 Implementar branching de erros tipados em `ParseProduct` — mínimo para passar
- [ ] 🔴 Escrever `TestHTTPCollector_Integration` com `httptest.Server` cobrindo os 4 critérios BDD: caminho feliz / seletor não casa / preço inválido / 5xx + timeout — deve falhar
- [ ] 🟢 Implementar `HTTPCollector.Collect` orquestrando bucket + UA + fetch + ParseProduct — mínimo para passar
- [ ] 🔵 Refatorar: extrair `traduzErroHTTP(err error, resp *http.Response) error` para função pura testável; revisar logs estruturados (slog com `source_id`, `store_id`, `status_code`, `duration_ms`)

**Arquivo(s) de teste a criar:**
- `internal/modules/scraping/collection/infrastructure/parser_test.go`
- `internal/modules/scraping/collection/infrastructure/token_bucket_test.go`
- `internal/modules/scraping/collection/infrastructure/http_collector_test.go`

**Comandos a rodar:**
- Adicionar deps: `go get github.com/PuerkitoBio/goquery golang.org/x/time/rate`
- Testes: `go test ./internal/modules/scraping/collection/... -race -cover`
- Suite full: `go test ./... -race -cover`
- Lint: `golangci-lint run`

## 🔄 Plano de rollback

- **Feature flag:** N/A — código consumido apenas pelo use case que ainda não está no worker
- **Reversibilidade de migration:** ✓ N/A — este Passo não tem migration
- **Passos de rollback:** remover arquivos de `internal/modules/scraping/collection/infrastructure/`; `go mod tidy` para limpar dependências; `git revert`
- **Raio de explosão:** zero em produção

## ✅ DoD (Definition of Done)

**Qualidade de código**
- [ ] Implementado conforme descrição técnica acima
- [ ] Segue convenções do `CLAUDE.md` (errors como valores tipados; ctx no 1º argumento de toda função de IO)
- [ ] Sem `panic`; falhas de rede traduzidas em erros tipados, nunca propagadas como erros genéricos
- [ ] Sem números mágicos: timeout, taxa default, burst default — constantes nomeadas no topo do pacote

**TDD e testes**
- [ ] Testes escritos antes da implementação
- [ ] Testes moram **neste** Passo
- [ ] Todos os testes novos passando: `go test ./... -race -cover`
- [ ] Nenhum teste existente quebrou (Passos 01 e 02 continuam verdes)
- [ ] Caminho feliz + 3 bordas (seletor não casa, preço inválido, 5xx/timeout) + UA realista enviado + token bucket respeitado, todos cobertos

**Segurança**
- [ ] Input dos seletores vem de `sources.selectors` (controlado pela aplicação, não pelo usuário final) — sem injection de XPath/CSS arbitrário do exterior
- [ ] HTML retornado pela loja tratado como input não-confiável: `goquery` aplica parsing seguro; nenhuma execução de JS (esse é o ponto de `http` vs `headless`)
- [ ] Sem segredo hardcoded (nenhum token nesta camada)
- [ ] Sem PII envolvido

**Performance**
- [ ] Timeout do `http.Client` configurado (default 10s) — evita worker travado
- [ ] `bytes.NewReader` + `goquery` é O(n) no tamanho do HTML; sem operação quadrática
- [ ] Histograma `collection_duration_seconds` instrumentado pelo `HTTPCollector` (label `strategy=http`) — embora a exposição via `/metrics` venha no Passo 04
- [ ] ✓ N/A — frontend não aplica

**Rate limit & throttling**
- [ ] `TokenBucketRegistry` enforce taxa por `store_id` (default 30 req/min, configurável)
- [ ] Limites alinhados com a restrição da Espec ("idempotência" no banco) e do Desenho ("rate-limit por loja")
- [ ] Loja retornando 429 → tratada como `ErrFetchFailed{Retryable: true}` para o river retentar
- [ ] ✓ N/A — sem chamada de cliente front

**Concorrência & idempotência**
- [ ] `TokenBucketRegistry` é thread-safe (mutex + `rate.Limiter` é concorrência-segura)
- [ ] Sem mutação compartilhada além do registry de buckets
- [ ] ✓ N/A — sem endpoint de criação para `Idempotency-Key`

**Testes de regressão**
- [ ] Caso "preço com formato BR (`R$ 1.234,56`)" tem teste explícito — bug típico em scraper iniciante
- [ ] Caso "preço esgotado" (texto onde se esperava número) tem teste explícito

**Revisão e merge**
- [ ] PR linkando Cenário + Passo 02 como dependência
- [ ] CI verde

## 🔗 Referências

- **Padrão de código a seguir:** `CLAUDE.md` seção "Estrutura" (módulo `<bounded>/<entity>/infrastructure/`)
- **Docs relevantes:** [goquery](https://github.com/PuerkitoBio/goquery), [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)
- **ADRs relacionados:** decisão 1 e 5 do `DESENHO.md` (default HTTP; anti-detecção MVP só com UA realista)
