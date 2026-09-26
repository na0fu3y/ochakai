---
type: Metric
title: 粗利
description: 完了した明細の実売価格から商品の原価を引いたもの(USD)。粗利率はそれを売上で割ったもの
tags: [sales, margin, finance, products]
generated: { by: analysis_agent/claude-fable-5, at: 2026-08-22T04:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-26T01:30:00Z }
status: stable
grain: item
synonyms: [粗利益, 売上総利益, gross profit, gross margin, 粗利率]
unit: USD
---

粗利は、[売上](/metrics/revenue.md)に数える明細それぞれについて、
`sale_price` から[商品](/tables/products.md)の `cost` を引いて合計した
ものである。粗利率は、粗利を同じ明細の売上で割ったもの。

- **原価は今のカタログの値である。** 原価の履歴は無く、過去の月の粗利も
  今日の原価で計算される。
- **明細の原価を在庫から引くときは、`inventory_item_id` で JOIN しない。**
  そのキーは売れた個体を指していない([inventory_item_id は売れた在庫を
  指さない](/insights/inventory-item-id.md))。原価は商品と同じなので、
  商品から引けば足りる。
- **返品の処理費・送料・決済手数料はこのデータのモデルに無い。** 粗利と
  呼んでいるが、実際には「売価 − 商品原価」だけである。

## 読み方

粗利率はカテゴリごとに固有の水準があり、月をまたいでもほとんど動かない。
生成器が商品ごとに原価率を固定しているためで、全体の粗利率も 1 年で
1 ポイントほどしか動かない。全体の粗利率が動いたときは、まず売れた
カテゴリの構成が変わったと読む。[明細単価](/metrics/average-item-price.md)
が動いたときと同じ原因で、同じ[カテゴリ別売上](/computations/revenue-by-category.md)
を見る。

検証済みの計算は[カテゴリ別粗利](/computations/gross-margin-by-category.md)
である。
