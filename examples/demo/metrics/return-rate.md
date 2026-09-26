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

返品率は、`Returned` の[明細](/tables/order-items.md)を、`Complete` と
`Returned` の明細の合計で割ったものである。分母は「返品の機会が閉じた
明細」、つまり届いたか、届いて返ってきたかのいずれかであり、全明細では
ない。

決めてあることが二つある。

- **明細単位で数え、注文単位では数えない。** 3点のうち1点が返った注文を、
  注文単位では 0 か 1 に丸めるしかない。今のデータでは一部返品は起きて
  いないが、規則は起きる日のために置いてある。
- **`Shipped` と `Processing` は分母に入れない。** まだ返品の機会が閉じて
  いない。

[完了率](/metrics/completion-rate.md)とは分母が違う。完了率は受注した
明細全体に対する割合で、返品はそこから抜ける流れの一つである。完了率が
落ちた月に返品率も上がっていれば、落ちた分は返品に流れている。そのとき
の行動の入口が[返品率の高い商品の見直し](/actions/review-high-return-products.md)
である。

水準が現実の EC と違う点にも注意がいる。全体の返品率はこのデータセット
では 3 割前後を動く。生成器がそう振っているだけなので、衣料品の実際の
返品率として読んではいけない。閾値を置くときに効いてくる。実務の感覚で
5% と置くと、候補が何百行も出る。だからアクションは `threshold` に
0.45 以上を要求する。

この指標は眺めるためではなく、行動の入力として使う。
