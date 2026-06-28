---
artefato: visao
criado_em: 2026-06-28T16:29:52Z
atualizado_em: 2026-06-28T16:29:52Z
---

# Visão — promo-scraper

> Documento único do projeto. Responde "**por que essa aplicação existe?**". Curto, denso, raramente alterado. Mudou? Versione.

## 👥 Para quem é

- **Usuário principal:** caçadores de promoção de hardware/eletrônicos — gamers montando rig, desenvolvedores atualizando setup (monitor, GPU, periférico), entusiastas de tech que acompanham SKU específico (ex.: "RTX 4070 por menos de R$ 3.500").
- **Contexto de uso:** mobile-first. Recebe alerta push (Telegram/WhatsApp/email) no celular quando o produto da sua watchlist cai de preço. A interação acontece em segundos: ele lê a notificação, decide, e clica no link de compra antes do estoque acabar.
- **Usuários secundários:** nenhum nesta fase. O produto é desenhado em torno do caçador individual — sem comunidade, sem moderação, sem painel de lojista.

## 😣 Que dor resolvemos

Promo de hardware de alto valor (placa de vídeo, processador, monitor gamer, SSD) costuma sumir em **minutos**. O caçador descobre 2 horas depois — pelo Twitter, Discord ou canal de Telegram cheio de ruído — e o estoque já acabou. Quando entra no site, ou a promo foi removida, ou só sobrou em uma cor/SKU que ele não quer, ou o pagamento via PIX já não está mais disponível.

A frustração é dupla: ele acompanha vários canais (subreddits, perfis no X, grupos de Telegram, sites tipo Promobit/Pelando) e ainda assim **perde a janela**. Os sites genéricos têm muito ruído de moda/mercado/casa; os canais de Telegram são lentos para postar e dependem de submissão humana. Quando a promo é boa de verdade — ela some antes de chegar até ele.

O produto resolve isso entregando **alerta automático em menos de 60 segundos** após a promo aparecer na loja, filtrado por SKU/categoria/preço-alvo que o usuário definiu, direto no canal que ele usa (Telegram, WhatsApp, email).

## 💡 Que oportunidade nos faz acreditar

Lojas brasileiras (Kabum, Pichau, Terabyte, Amazon BR, Mercado Livre) adotaram nos últimos anos camadas agressivas de anti-bot — Cloudflare Bot Management, Akamai, fingerprinting de TLS, desafios JS dinâmicos. Scrapers tradicionais (curl + parse de HTML) quebraram. Quem ainda monitora preço hoje faz isso de forma frágil ou semi-manual.

Em paralelo, as **técnicas para vencer essas defesas ficaram acessíveis**: proxies residenciais por uso (Bright Data, Smartproxy), navegadores headless reais com stealth (Playwright + plugins), pools de fingerprint TLS realistas. O que antes exigia infra de nível Bezos hoje cabe num servidor médio em VPS comum.

A janela é essa: existe demanda real (o caçador continua perdendo promo), a defesa do outro lado endureceu (concorrentes quebraram), e a tecnologia para sobreviver à defesa virou commodity. Quem montar o pipeline confiável agora ocupa o espaço antes que mais um competidor surja.

## 🎯 Métrica do norte

> **Uma só.** Se você quer rastrear duas, escolha a mais importante. A outra vira métrica de apoio.

- **Métrica:** latência p95 do alerta — tempo entre a promo aparecer na loja e o usuário receber o push no canal escolhido.
- **Baseline atual:** N/A — produto novo.
- **Meta de 12 meses:** **p95 < 60 segundos**.
- **Onde é lida:** dashboard Grafana sobre métricas Prometheus expostas pelo serviço (histograma `alert_dispatch_latency_seconds` com label por canal e loja).

## 🚫 O que esse produto **não é**

> Tão importante quanto o que ele é. Define a fronteira do que vamos recusar fazer.

- Não é site/app de promoção de categorias fora de hardware/eletrônicos (sem moda, mercado, casa, viagem, beleza). Foco é nicho.
- Não é marketplace, não processa pagamento, não intermedeia compra. Entregamos o alerta e redirecionamos via link de afiliado — a transação acontece na loja de origem.

## 📐 Decisões fundadoras

> Decisões de produto, técnicas ou estratégicas que moldam tudo daqui em diante. Versionadas: nunca apagar, sempre adicionar nova linha.

| Data | Decisão | Por quê | Por quem |
|---|---|---|---|
| 2026-06-28 | Foco exclusivo em hardware/eletrônicos no Brasil; sem outras categorias e sem outros países nesta fase. | Nicho onde a dor é mais aguda (promos relâmpago de alto valor) e onde os concorrentes generalistas (Promobit/Pelando) entregam pior por excesso de ruído. | Visão inicial |
| 2026-06-28 | Métrica do norte é latência p95 do alerta < 60s, não volume de promos nem MAU. | Velocidade é o único atributo que torna o produto utilizável — qualquer alerta tardio é equivalente a não alertar. | Visão inicial |
| 2026-06-28 | Sem marketplace próprio, sem checkout, sem pagamento. Apenas alerta + redirect via afiliado. | Manter o produto focado em uma única competência (scraping confiável + entrega rápida). Marketplace exigiria CNPJ-pagamento, antifraude, suporte de pedido — fora do escopo de valor. | Visão inicial |
