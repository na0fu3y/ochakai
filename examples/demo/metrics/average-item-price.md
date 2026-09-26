---
type: Metric
title: 明細単価
description: 売上になった明細1点あたりの平均実売価格(USD)。価格は商品ごとに固定なので、動いたらそれは売れた商品の構成が変わったということ
tags: [sales, pricing, products]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-18T02:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-09-20T01:10:00Z }
status: stable
grain: item
synonyms: [平均単価, average item price, 客単価]
unit: USD
---

`Complete` の[明細](/tables/order-items.md)の `sale_price` の平均である。
[売上](/metrics/revenue.md)を分解したときの三つ目の要因。

**価格は動かない。動くのは構成である。** 明細の `sale_price` は商品の
定価と全行で一致し、定価は商品ごとに一つしかない([products](/tables/products.md))。
値引きも値上げも無いので、明細単価が落ちた月は、安い商品が多く売れた月
である。見る先は[カテゴリ別売上](/computations/revenue-by-category.md)で、
単価の高いカテゴリ(Outerwear & Coats、Suits & Sport Coats、Jeans など)
の点数の比率が下がっていないかを見る。同じカテゴリの中でも、高い商品と
安い商品のどちらが売れたかで平均は大きく動く。

月ごとに数%〜1 割動き、その動きが売上の動きのかなりの部分を占める。
売上が落ちる月の半分ほどは、需要ではなくこの構成で落ちている
([売上が落ちるときの因果](/insights/why-revenue-falls.md))。

「客単価」と呼ばれることがあるが、ここでは明細 1 点あたりである。注文
あたりにしたいときは、注文あたりの点数(1.4 前後で安定)を掛ける。
なぜ安い商品に寄ったのか、つまり値引きや露出や在庫切れの記録は、この
データに無い([不足しているデータ](/insights/missing-data.md))。
