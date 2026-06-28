---
artefato: especificacao
visao_local: ../../VISAO.md
criado_em: 2026-06-28T16:37:53Z
atualizado_em: 2026-06-28T18:45:00Z
versao: v2
---

# Especificação — coleta-de-promo

> **Para você:** essa especificação descreve **o que essa capacidade faz**, em termos de comportamento observável. Decisões técnicas vão no `DESENHO.md` (opcional). Critérios verificáveis vão nos `CENARIO.md`. Trabalho de código vai nos `PASSO_*.md`.
>
> **Para o agente:** se você sentir que precisa de detalhe técnico para entender o que fazer, abra o `DESENHO.md` ou o `CENARIO.md` correspondente. Esta especificação não prescreve implementação.

## 🧭 Por que essa capacidade existe

Sem uma coleta confiável e rápida não existe promo para alertar. Esta capacidade é o **gargalo direto da métrica do norte** da Visão: latência p95 do alerta < 60s — a coleta consome a maior parte desse orçamento de tempo, e cada segundo perdido aqui é um segundo a menos para detectar, casar e entregar.

É a primeira capacidade a ser construída porque **destrava todas as demais**: watchlist sem promo não tem o que casar, alerta sem promo não tem o que disparar, histórico de preço sem coleta não tem o que registrar. Construir watchlist ou entrega antes de provar a coleta seria gastar UX em cima de uma fundação não-validada.

A oportunidade da Visão se materializa aqui: as lojas brasileiras de hardware/eletrônicos endureceram defesas anti-bot nos últimos anos; scrapers antigos quebraram. Quem reconstruir o pipeline agora — com técnicas modernas tornadas acessíveis (proxies residenciais, headless real com stealth) — ocupa o espaço antes que mais concorrentes surjam.

## ✅ O que o sistema faz

> Descrito como comportamento, não como tela ou endpoint. Cada item é uma afirmação verificável.

- Dado um catálogo de fontes (lojas + URLs + estratégia de coleta) já cadastrado, o sistema faz **polling contínuo** dessas URLs em intervalos definidos por fonte.
- Para cada coleta bem-sucedida, o sistema **extrai dados de produto** observáveis na página: SKU/identificador, título, preço atual, indicador de estoque, presença de badge promocional.
- O sistema **detecta variação relevante** entre o resultado da coleta atual e o último estado conhecido daquela URL: queda de preço, retorno de estoque, ou aparecimento de badge promocional.
- Quando detecta variação relevante, o sistema **materializa um evento `promo detectada`** no banco, com os dados normalizados e timestamp da detecção.
- O sistema **deduplica eventos**: a mesma promo (mesma URL, mesmo preço, mesmo SKU) coletada repetidamente dentro de uma janela curta gera **um único** evento, não N.
- A coleta é **disparada por job agendado interno** (scheduler do próprio serviço), sem dependência de trigger externo.
- O sistema **prossegue independentemente por fonte**: falha temporária ao coletar uma loja (timeout, 5xx, captcha) não interrompe coleta das demais.
- O sistema **expõe métricas de latência por etapa** (tempo de fetch, tempo de parse, tempo até evento materializado) para que o p95 do alerta seja rastreável até a coleta.

## 🚫 Fora de escopo (explícito)

> Não é "não pensamos nisso", é "decidimos não fazer agora". Cada item aqui evita scope creep.

- **Validação se a promo é real** (comparar com histórico de 90 dias, detectar preço falso inflado pela loja antes da promo). Vai para a capacidade `historico-de-preco`. Aqui a coleta materializa o evento mesmo se for "promo suspeita" — outra capacidade decide se vira alerta.
- **Casamento da promo com watchlist do usuário** (verificar se algum usuário pediu para ser avisado deste SKU/categoria/preço-alvo). Vai para a capacidade `match-de-watchlist`. Aqui a coleta produz o evento; quem consome é problema da próxima capacidade.

## 👥 Atores envolvidos

| Ator | Papel nesta capacidade |
|---|---|
| Scheduler interno | Inicia ciclos de coleta conforme intervalo definido por fonte. |
| Worker de coleta | Executa fetch + parse + detecção de variação para uma fonte específica. |
| Catálogo de fontes (consumido, não gerenciado) | Provê URLs + estratégia + intervalo por loja. O CRUD do catálogo é outra capacidade. |
| Lojas-alvo (sistema externo) | Respondem ao fetch com HTML/JSON do produto. Podem falhar, devolver captcha, ou bloquear. |
| Banco de eventos (Postgres) | Persiste `promo detectada` e o último estado conhecido por URL (para detectar variação no próximo poll). |

## 🔒 Restrições inegociáveis

> Tudo que **não pode ser violado**, mesmo sob pressão de prazo.

- **Regulatórias:** N/A — esta capacidade não trata dados pessoais de usuário. (LGPD aplica em capacidades que envolverem usuário cadastrado.)
- **Performance:** a coleta é o maior consumidor do orçamento de latência do alerta (meta p95 < 60s). Falhas de performance aqui inviabilizam a métrica do norte. Histograma `collection_duration_seconds` por loja é obrigatório.
- **Idempotência:** a mesma promo (mesma URL + mesmo preço + mesmo SKU) coletada N vezes dentro da janela de dedup deve gerar **exatamente um** evento `promo detectada`. Sem isso, o pipeline a jusante (match, alerta) é inundado.
- **Multi-tenant:** toda query no banco deve filtrar por `org_id`, conforme regra herdada do `CLAUDE.md` raiz. Não há exceção, mesmo para tabelas internas de catálogo/estado.
- **Segurança / dados sensíveis:** nenhum PII envolvido nesta capacidade — apenas dados públicos de página de produto.
- **Compatibilidade:** o schema do evento `promo detectada` é contrato consumido pelas capacidades `match-de-watchlist` e `historico-de-preco`. Mudança quebrante exige versionamento explícito.

## ❓ Questões em aberto

> Pontos onde ainda não decidimos. Cada item deve virar Decisão Registrada antes do primeiro Passo executar.

| Questão | Quem decide | Prazo |
|---|---|---|
| ~~Estratégia de scraping por loja: HTTP puro vs. headless com stealth.~~ **Resolvida em `DESENHO.md` (2026-06-28):** híbrido com default HTTP; `chromedp` caso-a-caso. | — | Resolvida |
| ~~Quais lojas exatas entram no MVP.~~ **Resolvida em `DESENHO.md` (2026-06-28):** Kabum, Pichau, Terabyte, Amazon BR e Mercado Livre. Probe empírico confirmou que **todas** exigem headless; HTTP simples fica como fast-path opt-in para fontes leves. | — | Resolvida |

## 📐 Decisões registradas

> Histórico vivo. Quando uma decisão cai, **nova linha** — não apaga a antiga.

| Data | Decisão | Por quê | Por quem |
|---|---|---|---|
| 2026-06-28 | Primeira capacidade do projeto a ser especificada e implementada é `coleta-de-promo`. | É a fundação: sem coleta não há promo, e qualquer investimento em watchlist/alerta/UX antes seria construir sobre fundação não-validada. | Especificação inicial |
| 2026-06-28 | Coleta materializa eventos mesmo quando a promo for suspeita; validação de "promo real" é responsabilidade da capacidade `historico-de-preco`. | Separação de responsabilidades: coleta = observação fiel do mundo; validação = decisão. Permite que a auditoria veja "o que apareceu na loja" mesmo se o alerta não for disparado. | Especificação inicial |
| 2026-06-28 | Sistemas externos do MVP são apenas as lojas-alvo. Sem dependência de provider pago de proxy residencial nem de serviço gerenciado de browser headless nesta fase. | Manter custo unitário baixo e validar viabilidade técnica antes de assumir custo fixo de provider. Se a taxa de bloqueio inviabilizar, revisita em decisão técnica posterior. | Especificação inicial |
| 2026-06-28 | Restrições registradas nesta capacidade são performance crítica e idempotência. Multi-tenant (`org_id`) é regra global do `CLAUDE.md` e aplica sem precisar ser repetida aqui. | Foco da Especificação é o que é específico desta capacidade; regras globais ficam onde já estão para evitar duplicação que envelhece mal. | Especificação inicial |
| 2026-06-28 | Pendência "HTTP vs. headless" resolvida em `DESENHO.md`: scraping híbrido com default HTTP (`net/http` + `goquery`), `chromedp` caso-a-caso por fonte. | Desenho técnico detalha trade-offs e alternativas rejeitadas; cabe registrar aqui que esta decisão saiu do estado "aberto". | Desenho técnico |
| 2026-06-28 | Lojas-alvo do MVP: **Kabum, Pichau, Terabyte, Amazon BR, Mercado Livre**. Default de `sources.strategy` para todas elas passa a ser `headless`. HTTP simples vira fast-path opt-in para fontes confirmadamente leves (RSS, sitemap, lojas sem anti-bot). | Probe empírico (curl com UA realista, 2026-06-28) mostrou: ML redireciona qualquer URL com preço para `/gz/account-verification`; Terabyte responde 403 com challenge Cloudflare ("Just a moment..."); Kabum/Pichau são notoriamente protegidos pelo mesmo perfil. A premissa original do DESENHO ("começamos 100% em HTTP") não sobrevive ao mundo real — o anti-bot subiu nas lojas BR-hardware. | Descoberta operacional |

## 🔗 Cenários vinculados

> Listado automaticamente pelo `spec-status`. Não preencher manualmente.

-
