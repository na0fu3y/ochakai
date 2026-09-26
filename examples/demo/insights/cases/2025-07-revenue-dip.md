---
type: Insight
title: 2025 年 7 月の売上減の診断
description: 受注点数は 7.5% 伸びたのに売上が前月を割った。落としたのは明細単価で、単価の高いカテゴリの比率が下がっていた。2026-09-26 のスナップショットでの診断(draft)
tags: [sales, revenue, diagnosis, case]
generated: { by: analysis_agent/claude-fable-5, at: 2026-09-26T13:50:00Z }
status: draft
---

「2025 年 7 月の売上が前月を割っているのはなぜか」という問いに、
[メトリクスが沈んだときの調べ方](/skills/diagnose-a-metric.md)の手順で
答えたものである。エージェントが書いた draft で、まだ誰も確かめていない。

**この数字は 2026-09-26 13:47 UTC のスナップショットのものである。**
データセットは毎日作り直されるので、別の日に走らせれば 7 月が前月を
割っていないこともありうる([thelook_ecommerce](/datasets/thelook-ecommerce.md))。
数字は下の receipt と一緒にだけ意味を持つ。

## 結論

売上は前月比 −1.1%。**需要は伸びていて、落としたのは構成である。**

| 要因 | 6 月 | 7 月 | 対数の変化 |
|---|---|---|---|
| [受注点数](/metrics/booked-items.md) | 3,409 | 3,676 | +0.075 |
| [完了率](/metrics/completion-rate.md) | 0.246 | 0.250 | +0.014 |
| [明細単価](/metrics/average-item-price.md) | 61.88 | 55.93 | **−0.101** |
| [売上](/metrics/revenue.md) | 51,979.66 | 51,395.33 | −0.011 |

明細単価が 1 割下がり、受注点数と完了率の伸びを打ち消した。

## 構成の中身

[カテゴリ別売上](/computations/revenue-by-category.md)で 6 月と 7 月を
比べると、単価の高いカテゴリ(Outerwear & Coats、Suits & Sport Coats、
Suits、Jeans、Blazers & Jackets、Dresses)の点数の比率が 22.3% から
18.7% に下がり、安いカテゴリ(Intimates、Tops & Tees、Shorts)が伸びて
いた。売上の減りが大きかったのは Dresses(−2,120)、Outerwear & Coats
(−1,458)、Suits & Sport Coats(−802)。Dresses はカテゴリの中の単価も
113.75 から 53.58 に下がっていて、高い商品が売れなかった月だった。

[流入元別の受注](/computations/orders-by-session-source.md)でも、どの経路の
受注も減っていない(Organic だけが横ばい)。需要側の説明は要らない。

## 取れる行動と、止まった場所

- 価格は商品ごとに固定なので、単価は値下げでは動いていない。売れ筋の
  単価を動かす行動はこのバンドルに無い。
- 高単価カテゴリが売れなかった理由(露出、在庫切れ、販促)の記録は
  無く、ここで止まる([不足しているデータ](/insights/missing-data.md))。
- 返品は増えていない(`returned_share` 0.097 → 0.098)ので、
  [返品率の高い商品の見直し](/actions/review-high-return-products.md)は
  この月の答えではない。

## receipt

| 計算 | job_id | 行数 |
|---|---|---|
| [月次売上の要因分解](/computations/revenue-drivers.md)(2025-06-01〜2025-08-01) | `ochakai-example:US.demo_case_2025_07_revenue_drivers` | 2 |
| [カテゴリ別売上](/computations/revenue-by-category.md)(同) | `ochakai-example:US.demo_case_2025_07_revenue_by_category` | 52 |
| [流入元別の受注](/computations/orders-by-session-source.md)(同) | `ochakai-example:US.demo_case_2025_07_orders_by_session_source` | 10 |

どれも concept の `# Computation` を書き換えずに実行した。流入元別の受注は
draft の計算なので、その行の数字は信用で受け取る数字である。
