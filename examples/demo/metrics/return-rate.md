---
type: Metric
title: 返品率
description: 返品の機会が閉じた明細のうち、実際に返ってきた明細の割合
tags: [sales, returns, quality]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-26T03:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-31T01:20:00Z }
status: stable
grain: item
synonyms: [return rate]
unit: ratio
---

返品率は、`Returned` の[明細](/tables/order-items.md)を、
`Complete` と `Returned` の明細の合計で割ったものである。分母は
「返品の機会が閉じた明細」— 届いたか、届いて返ってきたか — であって、
全明細ではない。

決めてあることが三つある:

- **明細単位で数える。注文単位ではない。** 3点のうち1点が返った注文
  を、注文単位は 0 か 1 に丸めるしかない。明細単位なら 1/3 のまま
  数えられる([完了した注文](/glossary/completed-order.md)の一部返品の
  項)。
- **`Shipped` と `Processing` は分母に入れない。** まだ返品の機会が
  閉じていない。入れると、出荷が伸びている期間の返品率が実態より低く
  出る。
- **この数字も過去に遡って動く。** status に履歴が無いので、先月の
  返品率は今日も動いている。時点を固定した比較には receipt を使う
  こと([thelook_ecommerce データセット](/references/thelook-dataset.md))。

**水準が現実の EC と違う。** 全体の返品率はこの世界では 4 分の 1 から
3 分の 1 のあたりを動く — 合成データの生成器がそう振っているだけで、
衣料品の実際の返品率として読んではいけない。効いてくるのは閾値を置く
ときで、実務の勘で 5% と置くと候補が何百行も出る。
[返品率の高い商品の見直し](/actions/review-high-return-products.md)が
`threshold` に 0.45 以上を要求するのはそのためである。

この指標が閾値を超えた商品を洗い出して取り扱いの見直しにかけるのが、
[返品率の高い商品の見直し](/actions/review-high-return-products.md)で
ある — 数字を眺めるための指標ではなく、行動の入力である。
