---
type: Metric
title: 売上
description: 完了した明細の実売価格を、明細の作成時刻で束ねて合計したもの(USD)
tags: [sales, revenue, finance]
sources:
  - id: rev-policy
    resource: https://wiki.example.co.jp/finance/revenue-recognition
    title: 売上計上ポリシー (FY2026)
    author: human:tanaka@example.co.jp
    last_modified: "2026-07-24"
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
で束ねて合計したものである。これが売上の定義である。式は
`SUM(sale_price)` だけなので、この数字で揉めるときは、たいていどの行が
入るかで揉めている。

自明ではない点が三つある。

- **USD で、税という概念が無い。** `sale_price` は実売価格そのもので、
  このデータセットのモデルに税も送料も存在しない。「税込か税抜きか」
  という問い自体が無いので、書いておかないと誰かが探しに行くことになる。
- **返品は遡って消える。** 明細が `Returned` になると、その明細は
  *売れた月の*売上から抜ける。相殺の列は作らない。[^rev-policy]
  過去の月の数字は、たいていこれで動く。
- **受注ベース(GMV)とは別物である。** 受注した瞬間の合計が必要な
  議論では[月次受注額](/queries/sales/monthly-bookings.md)を見る。
  deprecated だが、GMV と売上を混ぜた資料を読むときのために残してある。

## 呼び名

社内ではこの数字を「売上」と呼ぶ。ウェアハウスの側では `revenue` という
列名で出てくるので、両方の名前で引けるようにしてある。「トップライン」も
同じものを指す。GMV はこの指標ではない。

検証済みの計算方法は[月次売上](/queries/sales/monthly-revenue.md)で
ある。動きを良し悪しで読む前に、
[売上の読み方](/insights/reading-revenue.md)を読むこと。この系列には
成長が作り込まれていて、上がっていること自体には情報が無い。客が戻って
きたかどうかは[リピート購入率](/metrics/repeat-purchase-rate.md)、
商品が返ってきているかどうかは[返品率](/metrics/return-rate.md)が扱う。

[^rev-policy]: 売上計上ポリシー (FY2026)
