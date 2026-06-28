---
artefato: cenario
espec_local: ../../ESPEC.md
criado_em: 2026-06-28T16:53:30Z
atualizado_em: 2026-06-28T16:53:30Z
---

# Cenário — detectar-queda-de-preco-em-fonte-http

> **Para você:** este cenário descreve **um comportamento testável** em formato BDD. Cada critério ou passa ou não passa, sem interpretação.
>
> **Para o agente:** comportamento mora aqui. Implementação mora no Passo. Se você precisa de detalhe de função ou arquivo para entender o que validar, está lendo o artefato errado — abra o Passo filho.

## 🧭 Contexto

> Por que esse cenário existe. Conexão com a Especificação pai.

- **Capacidade pai:** `coleta-de-promo` (ver `../../ESPEC.md`).
- **Quando esse fluxo ocorre:** tick natural do scheduler `river` — uma fonte com `strategy='http'` atinge `now() >= last_collected_at + interval_seconds` e é enfileirada para nova coleta. Acontece continuamente durante a vida do worker.
- **Quem inicia:** scheduler interno (`river`). O caminho via disparo manual de admin não é coberto por este cenário.
- **Por que este cenário primeiro:** é o **caminho feliz fundador** da capacidade — sem ele funcionar, nenhum outro cenário (dedup, resiliência multi-loja, headless) tem sobre o que rodar. Define o contrato de saída (`promo_events`) que as capacidades a jusante consomem.

## 📜 Fluxo principal

> Passo a passo do caminho feliz. O que o sistema responde a cada estímulo.

1. O scheduler identifica que uma fonte cadastrada com `strategy='http'` está pronta para nova coleta (`now() >= last_collected_at + interval_seconds`) e enfileira um job de coleta para essa fonte.
2. O worker pega o job e executa fetch HTTP da URL declarada na fonte, com User-Agent realista do pool rotativo e respeitando o token bucket configurado para a loja correspondente.
3. O sistema aplica os seletores declarados em `sources.selectors` sobre o HTML retornado e extrai os campos observáveis: SKU, título, preço atual, indicador de estoque, indicador de badge promocional.
4. O sistema compara o `preco` extraído com o `preco` armazenado em `sources.last_snapshot` e detecta queda relevante (`preço atual < preço anterior` em ≥ 1 centavo).
5. O sistema materializa um evento `promo detectada` na tabela `promo_events`, preenchendo todos os campos observáveis (sku, titulo, preco, preco_anterior, moeda, estoque_disponivel, badge_promo, url, detected_at, source_id, store_id, org_id).
6. O sistema atualiza a fonte: novo `last_snapshot` refletindo o poll atual, `last_collected_at = now()`, `last_error = NULL`, `version` incrementado.

**Estado final esperado:** 1 linha nova em `promo_events` correspondente à promo detectada; `sources.last_snapshot` reflete o estado pós-poll; métrica `collection_duration_seconds{store=<X>}` registrada com o tempo total do ciclo.

## ✅ Critérios de aceite (BDD)

> Formato `Dado [contexto], Quando [ação], Então [resultado]`.
>
> ⚠️ **Não escreva aqui:** nomes de função, paths de arquivo, "criar endpoint X". Comportamento, não código.

- [ ] **Dado** que existe uma fonte com `strategy='http'` e `last_snapshot.preco = 100,00`, e a loja-alvo responde 200 OK com HTML em que os seletores extraem `preco = 90,00`, **Quando** o scheduler enfileira a coleta dessa fonte e o worker processa o job, **Então** uma linha é inserida em `promo_events` com `preco=90,00`, `preco_anterior=100,00`, e os demais campos observáveis preenchidos, e `sources.last_snapshot` passa a refletir o poll atual.
- [ ] **Dado** que existe uma fonte com `strategy='http'` e `last_snapshot.preco = 100,00`, **Quando** o worker coleta e o seletor de preço não casa nada (preço extraído é NULL ou vazio), **Então** nenhuma linha é inserida em `promo_events`, `sources.last_snapshot` permanece inalterado, e `sources.last_error` é populado com mensagem identificando o seletor que falhou.
- [ ] **Dado** que existe uma fonte com `strategy='http'` e `last_snapshot.preco = 100,00`, **Quando** o worker coleta e o preço extraído do HTML é `0,00` ou negativo, **Então** nenhuma linha é inserida em `promo_events`, `sources.last_snapshot` permanece inalterado, `sources.last_error` é populado, e a métrica de erro da coleta é incrementada.
- [ ] **Dado** que existe uma fonte com `strategy='http'` e a loja-alvo retorna timeout ou status 5xx, **Quando** o worker tenta coletar, **Então** o job é re-enfileirado pelo `river` com backoff exponencial e jitter até no máximo 5 tentativas; em **nenhuma** das tentativas é inserida linha em `promo_events` nem é atualizado `sources.last_snapshot`; após esgotadas as 5 tentativas, `sources.last_error` é populado com a falha externa observada.

## 🚫 O que esse cenário **não** cobre

> Lista bordas / fluxos que pertencem a outros cenários ou estão fora de escopo da Espec inteira.

- Dedup de evento dentro da mesma janela temporal de 30 minutos. Comportamento de idempotência será especificado no cenário `deduplicar-evento-no-mesmo-bucket`.

## 🧪 Roteiro de validação manual

> Passos para validar contra os critérios em ambiente local ou staging. Cada linha: ação + resultado esperado.

| # | Ação | Resultado esperado | ✓ |
|---|---|---|---|
| 1 | Subir o serviço local com Postgres rodando, migrations aplicadas, `river` configurado e um mock server HTTP local apontando para fixture HTML do produto-alvo | Serviço health-check verde; `river` reporta queue ativa | ☐ |
| 2 | Inserir manualmente em `sources` uma linha com `org_id` de teste, `strategy='http'`, `url` apontando para o mock, `interval_seconds=5`, `selectors` casando os campos da fixture, `last_snapshot={"preco":100.00}`, `enabled=true` | Linha persistida; `last_collected_at` NULL ou antigo o suficiente para ser elegível ao próximo tick | ☐ |
| 3 | Configurar o mock server para responder 200 OK com HTML do produto a `preco=90.00`, estoque disponível, badge promocional presente | Mock responde com payload esperado quando consultado via `curl` | ☐ |
| 4 | Aguardar até o próximo tick do scheduler (≤ `interval_seconds + jitter`) | Worker processa um job de coleta para a fonte de teste | ☐ |
| 5 | Consultar `promo_events` filtrando pelo `org_id` de teste | Exatamente 1 linha com `preco=90.00`, `preco_anterior=100.00`, `source_id` correspondente, e `detected_at` próximo de agora | ☐ |
| 6 | Consultar a fonte de teste em `sources` | `last_snapshot.preco = 90.00`, `last_collected_at` próximo de agora, `last_error = NULL`, `version` incrementado | ☐ |
| 7 | Inspecionar o endpoint de métricas do worker (Prometheus) | Histograma `collection_duration_seconds` registrou amostra para o `store_id` da fonte de teste | ☐ |
| 8 | Reconfigurar o mock para responder timeout. Aguardar próximo tick | Job é re-enfileirado pelo `river` com backoff; nenhuma linha nova em `promo_events`; `last_snapshot` inalterado | ☐ |
| 9 | Após 5 tentativas esgotadas | `sources.last_error` populado com a falha; sem evento em `promo_events` | ☐ |
| 10 | Reconfigurar o mock para responder HTML sem o seletor de preço. Aguardar próximo tick | `last_snapshot` inalterado; `last_error` populado com referência ao seletor não-casado; sem evento | ☐ |

## 📊 Como medimos pós-deploy

> Diferente dos critérios (que respondem "funciona?"), isso responde "foi adotado e teve impacto?".

- **Métrica:** `collection_duration_seconds{strategy="http"}` — p95 do tempo total do ciclo de coleta para fontes HTTP (do dequeue do job até o commit de `promo_events`/`sources`).
- **Baseline:** N/A — capacidade nova.
- **Meta:** p95 < 5 segundos. Justificativa: orçamento total do alerta é p95 < 60s (métrica do norte da Visão); coleta deve consumir, no máximo, parte significativamente menor que o orçamento total para deixar margem para match, geração de payload e entrega ao canal.
- **Onde acompanhar:** dashboard Grafana sobre Prometheus, painel `coleta-de-promo` filtrado por `strategy=http`.

## 🔗 Passos vinculados

> Listado automaticamente pelo `spec-status`. Não preencher manualmente.

-
