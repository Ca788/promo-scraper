---
artefato: desenho
espec_local: ./ESPEC.md
criado_em: 2026-06-28T16:45:00Z
atualizado_em: 2026-06-28T18:50:00Z
---

# Desenho técnico — coleta-de-promo

> **Documento opcional.** Crie quando a Especificação envolve decisões técnicas não-óbvias (escolha de stack, modelo de dados complexo, integração arriscada). Capacidades simples (CRUD trivial) não precisam — vão direto da Espec para Cenários.
>
> **Para você:** este documento registra **o que foi considerado**, **o que foi escolhido**, **por quê**. Para que, em 6 meses, ninguém precise re-derivar a decisão.
>
> **Para o agente:** detalhes de arquivos e funções **não** moram aqui — moram no Passo. Aqui mora a justificativa.

## 🎯 Abordagem escolhida

A coleta adota **scraping híbrido com default HTTP**. Cada linha do catálogo de fontes declara explicitamente sua `strategy` (`http` ou `headless`). Começamos com 100% das fontes em `http`; uma fonte é promovida a `headless` apenas quando o anti-bot da loja-alvo quebra o caminho HTTP — escolha caso-a-caso, não global. Isso minimiza custo unitário (HTTP é ordens de grandeza mais barato em latência e recursos), preserva a meta de p95 < 60s do alerta, e escala incrementalmente conforme cada loja exigir.

A pilha aderente ao stack Go do projeto: `net/http` da stdlib + `goquery` para parse de HTML no caminho HTTP; `chromedp` (puro Go, controla Chrome via CDP) no caminho headless, quando necessário. O scheduling roda sobre `river` — fila Postgres-native — evitando introduzir Redis no stack nesta fase. Rate-limit por loja é implementado via token bucket em memória, dimensionado por `store_id` e configurável.

O contrato de saída desta capacidade é o evento `promo detectada`, persistido na tabela `promo_events`. Esse contrato é consumido pelas capacidades `match-de-watchlist` e `historico-de-preco` — qualquer mudança quebrante exige versionamento explícito.

## 🔀 Alternativas consideradas

| Alternativa | Prós | Contras | Por que rejeitamos |
|---|---|---|---|
| Default `headless` (Playwright/chromedp para todas as lojas desde o início) | Robusto contra anti-bot; pipeline único e uniforme; resiste a SPAs e renderização JS | 10–100x mais lento por fonte; consumo de memória/CPU alto; latência ataca diretamente o p95 < 60s | Custo da uniformidade não compensa enquanto a maioria das lojas-alvo ainda serve HTML estático |
| Só `http` (sem headless de jeito nenhum no MVP) | Mais simples; menor superfície de bugs e dependências | Exclui qualquer loja que use Cloudflare desafio JS ou render dinâmico de preço — pode tirar Kabum/Pichau do MVP | Risco de excluir lojas-chave do nicho hardware é inaceitável |
| Só `headless` (toda fonte via chromedp) | Pipeline único | Mesma latência/custo do default headless, sem o upside de uniformidade arquitetural | Mesmo motivo do default headless |
| `Colly` como framework de scraping | Já traz rate-limit, dedup, queue prontos | Acopla a um framework e a um modelo de execução que não casa com `river`; dificulta integração com a stack chi+pgx+sqlc | Preferimos compor primitivas da stdlib que já conhecemos e controlar todos os pontos de extensão |
| Cron interno (goroutine + ticker em `cmd/worker`) lendo `sources` em vez de `river` | Zero dependência extra; controle total | Sem retry automático, sem dead-letter, sem visibilidade de jobs em andamento; reinventar o que `river` já oferece | `river` é Postgres-native e está alinhado com o stack — custo de dependência é baixo, ganho operacional é alto |
| `asynq` (fila Redis-based) no lugar de `river` | Latência de dispatch potencialmente menor; ecossistema maduro | Adiciona Redis ao stack — operação, monitoração e custo extra | Não há demanda de latência de dispatch que justifique introduzir Redis nesta fase |

## 📦 Interfaces e contratos chave

### Tabela `sources` (consumida por esta capacidade, gerenciada por `cadastro-de-fonte`)

```
id                bigserial PK
org_id            uuid NOT NULL  -- multi-tenant, indexado
store_id          bigint NOT NULL FK  -- Kabum, Pichau, etc.
url               text NOT NULL
strategy          text NOT NULL CHECK (strategy IN ('http','headless'))
interval_seconds  integer NOT NULL
selectors         jsonb NOT NULL  -- {"preco":"...", "estoque":"...", "badge":"..."}
enabled           boolean NOT NULL DEFAULT true
last_collected_at timestamptz
last_snapshot     jsonb  -- {"preco":..., "estoque":..., "badge":..., "titulo":..., "sku":...}
last_error        text
version           integer NOT NULL DEFAULT 1  -- optimistic locking
created_at        timestamptz NOT NULL DEFAULT now()
updated_at        timestamptz NOT NULL DEFAULT now()

UNIQUE (org_id, url)
INDEX (enabled, last_collected_at)  -- scheduler picking
```

### Evento `promo detectada` — tabela `promo_events`

```
id                bigserial PK
org_id            uuid NOT NULL  -- multi-tenant
source_id         bigint NOT NULL FK sources(id)
store_id          bigint NOT NULL FK stores(id)
sku               text NOT NULL  -- identificador na loja
titulo            text NOT NULL
preco             numeric(12,2) NOT NULL
preco_anterior    numeric(12,2)
moeda             text NOT NULL DEFAULT 'BRL'
estoque_disponivel boolean NOT NULL
badge_promo       boolean NOT NULL
url               text NOT NULL
detected_at       timestamptz NOT NULL DEFAULT now()

-- Idempotência: dedup por janela temporal de 30min
dedup_bucket      timestamptz GENERATED ALWAYS AS (date_trunc('hour', detected_at) + interval '30 min' * (extract(minute from detected_at)::int / 30)) STORED
UNIQUE (org_id, source_id, preco, dedup_bucket)

INDEX (org_id, detected_at DESC)  -- consumo cronológico por match/histórico
INDEX (org_id, source_id, detected_at DESC)
```

### Contrato consumido por capacidades a jusante

`match-de-watchlist` e `historico-de-preco` consomem `promo_events` lendo por `org_id + detected_at`. Esta capacidade **não** publica em fila externa nesta fase — fan-out fica como decisão de capacidade consumidora.

### Estratégia de coleta (interface interna Go, em pseudo-código)

```
type Collector interface {
    Collect(ctx context.Context, src Source) (Snapshot, error)
}

// Implementações: HTTPCollector (net/http + goquery) e HeadlessCollector (chromedp).
// Selecionada por src.Strategy no worker do river.

type Snapshot struct {
    SKU, Titulo string
    Preco       decimal.Decimal
    Estoque     bool
    Badge       bool
    ColetadoEm  time.Time
}
```

## 🗄️ Considerações de dados

- **Volume esperado (fase 1):** ~50 lojas × ~1000 URLs cada = ~50k linhas em `sources`. Polling com intervalo médio de 5 minutos gera ~10M polls/dia → mas evento só materializa em variação relevante. Estimativa de `promo_events`: 50k–500k/dia. Sustentável em Postgres comum por anos.
- **Particionamento:** `promo_events` particionada por mês (`detected_at`) já no MVP — barata de configurar agora, cara de fazer depois.
- **Índices:** `sources(enabled, last_collected_at)` é o índice quente do scheduler. `promo_events(org_id, detected_at DESC)` é o índice quente do consumo por match/histórico.
- **`last_snapshot` na tabela `sources`:** decisão consciente de manter o último estado como coluna `jsonb` na própria `sources` (não em tabela separada). Trade-off: simplifica a detecção de variação (1 read + 1 update por poll), em troca de não persistir histórico completo aqui. `historico-de-preco` (outra capacidade) será responsável por append-only se/quando precisar.
- **Migrações:** zero-downtime via `golang-migrate`. Adicionar coluna `nullable` primeiro, backfill em job idempotente, depois constraint. Nunca rename direto.
- **Lock otimista:** `sources.version` evita worker A sobrescrever `last_snapshot` de worker B que polou ao mesmo tempo (não deveria acontecer com `river`, mas defesa em profundidade).

## ⚠️ Riscos e mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|---|---|---|---|
| Ban de IP por loja sem proxy residencial — uma única loja bloqueia o IP de saída do worker | Média | Alto | Tomar como sinal forte: alarmar quando taxa de 4xx/captcha de uma loja > X% em janela de Y minutos. Estar pronto para introduzir provider de proxy residencial como decisão técnica futura — não no MVP, mas previsto. |
| Loja muda HTML/seletores sem aviso — coleta para de extrair `preco` (vira NULL ou string vazia) | Alta | Alto | Alarmar quando preço extraído é NULL/zero em N polls consecutivos para uma fonte. Selectors em `jsonb` permitem hotfix sem deploy (atualizar `sources.selectors` para a URL afetada). |

## ❓ Questões técnicas em aberto

- [ ] Quais lojas exatas entram no MVP? Sugestão para discussão: Kabum, Pichau, Terabyte (lojas dedicadas a hardware no nicho BR); Amazon BR e Mercado Livre ficam para fase 2 (anti-bot mais agressivo, exigem headless).

## 📐 Log de decisões

| Data | Decisão | Por quê | Por quem |
|---|---|---|---|
| 2026-06-28 | Scraping híbrido com **default HTTP** (`net/http` + `goquery`); `chromedp` apenas quando uma fonte específica exigir. Estratégia declarada por linha em `sources.strategy`. | Custo unitário de HTTP é ordens de grandeza menor; meta de p95 < 60s não tolera headless universal; promoção caso-a-caso ataca o anti-bot só onde ele aparece. | Desenho inicial |
| 2026-06-28 | Scheduling via `river` (fila Postgres-native), não cron interno nem `asynq`. | river traz retry, dead-letter e visibilidade prontos; alinhado ao stack Postgres; sem custo de adicionar Redis. | Desenho inicial |
| 2026-06-28 | Idempotência via unique constraint em `(org_id, source_id, preco, dedup_bucket)` com `dedup_bucket` = janela fixa de 30 minutos, no nível do banco. | Garantia em SQL é mais robusta que lock distribuído; janela fixa de 30min absorve flutuações sem perder promos reais (queda > 30min é evento novo de qualquer forma); sem dependência de Redis. | Desenho inicial |
| 2026-06-28 | Detecção de variação = qualquer queda de preço ≥ 1 centavo entre `last_snapshot` e poll atual. | Materializar barato e delegar para `historico-de-preco` a decisão de "promo real vs. falsa". Mantém esta capacidade focada em observar, não decidir. | Desenho inicial |
| 2026-06-28 | Anti-detecção MVP = apenas pool rotativo de User-Agent realista. Pool de proxies, jitter, headers ricos, TLS fingerprinting ficam fora desta fase. | Cada técnica tem custo de implementação e operação; introduzir só quando o sinal de bloqueio justificar; UA rotativo cobre 80% dos casos triviais sem custo relevante. | Desenho inicial |
| 2026-06-28 | `last_snapshot` armazenado como `jsonb` na própria tabela `sources` (e não em tabela separada). | Simplifica detecção de variação (1 read + 1 update por poll); histórico append-only fica em `historico-de-preco`, evita duplicar responsabilidade. | Desenho inicial |
| 2026-06-28 | Tabela `promo_events` particionada por mês em `detected_at` já no MVP. | Particionamento é barato fazer agora e caro depois; volume projetado já justifica. | Desenho inicial |
