---
type: Metric
title: 売上
description: 完了した明細の実売価格を、明細の作成時刻の月で束ねて合計したもの(USD)。受注点数 × 完了率 × 明細単価に分解できる
tags: [sales, revenue, finance]
sources:
  - id: rev-policy
    resource: https://wiki.example.co.jp/finance/revenue-recognition
    title: 売上計上ポリシー (FY2026)
    author: human:tanaka@example.co.jp
    last_modified: "2026-07-24T00:00:00Z"
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T05:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-29T02:15:00Z }
status: stable
grain: item
synonyms: [トップライン, top line, revenue]
unit: USD
---

売上は、[完了した注文](/glossary/completed-order.md)に属する
[明細](/tables/order-items.md)の `sale_price` を、明細の `created_at`
で束ねて合計したものである。これが売上の定義である。[^rev-policy]
式は `SUM(sale_price)` だけなので、この数字で揉めるときは、たいていどの行が入るかで揉めている。

# 分解

売上は三つの数の積に、ちょうど分かれる。

```
売上 = 受注点数 × 完了率 × 明細単価
```

| 要因 | 何を表すか | 動いたときに見る先 |
|---|---|---|
| [受注点数](/metrics/booked-items.md) | 需要。状態を問わず受注した明細の数 | 流入元([流入元別の受注](/computations/orders-by-session-source.md)) |
| [完了率](/metrics/completion-rate.md) | 受注のうち売上になった割合 | どの状態に流れたか(返品・キャンセル・未完了) |
| [明細単価](/metrics/average-item-price.md) | 売上になった明細の平均価格 | カテゴリの構成([カテゴリ別売上](/computations/revenue-by-category.md)) |

月ごとの三つの値と前月からの変化は[月次売上の要因分解](/computations/revenue-drivers.md)
が一度に出す。売上が落ちたときにどの順で見るかは
[売上が落ちるときの因果](/insights/why-revenue-falls.md)にある。

# 自明ではない点

- **USD で、税という概念が無い。** `sale_price` は実売価格そのもので、
  このデータセットのモデルに税も送料も存在しない。
- **受注ベース(GMV)とは別物である。** 受注点数に単価を掛けた合計は
  [月次受注額](/computations/monthly-bookings.md)で、deprecated だが
  FY2025 の資料を読むために残してある。売上は常にその 3 割ほどになる。
- **過去の月も動く。** 理由は返品ではなく、データセットが履歴ごと毎日
  作り直されることである([thelook_ecommerce](/datasets/thelook-ecommerce.md))。

# 呼び名

社内ではこの数字を「売上」と呼ぶ。ウェアハウスの側では `revenue` という
列名で出てくるので、両方の名前で引けるようにしてある。「トップライン」も
同じものを指す。GMV はこの指標ではない。

検証済みの計算方法は[月次売上](/computations/monthly-revenue.md)である。
動きを良し悪しで読む前に[売上の読み方](/insights/reading-revenue.md)を
読むこと。この系列には成長が作り込まれていて、上がっていること自体には
情報が無い。

[^rev-policy]: 売上計上ポリシー (FY2026)
