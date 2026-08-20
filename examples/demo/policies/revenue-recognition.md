---
type: Policy
resource: https://wiki.example.co.jp/finance/revenue-recognition
title: 売上計上ポリシー (FY2026)
description: どの明細を、いつ売上として数えるか。そして過去の数字が動くことをどう扱うか
tags: [finance, revenue, policy]
generated: { by: human:sato@example.co.jp, at: 2026-07-24T06:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T02:05:00Z }
status: stable
---

数字が従っている規則である。この concept の resource が指す経理の wiki
ページの写しで、正はあちらのページである。ここに置いてあるのは、
[売上](/metrics/revenue.md)を読んでいるエージェントが、ナレッジベースを
出ずに規則を参照できるようにするためである。

FY2026 について決めたのは次の三つである。

1. **売上は配達完了ベースである。** 数えるのは `status = 'Complete'` の
   [明細](/tables/order-items.md)だけである
   ([完了した注文](/glossary/completed-order.md))。受注時点でも出荷時点
   でもない。受注ベースの合計は
   [月次受注額](/queries/sales/monthly-bookings.md)と呼び、売上とは
   呼ばない。
2. **返品は遡って消し、相殺しない。** 明細が `Returned` になった時点で、
   その明細は売れた月の売上から抜ける。マイナスの行や相殺の列は作らない。
   つまり過去の月の数字は動く。動かない数字が必要な場面(締め、社外
   報告)では、締めの時点で
   [月次売上](/queries/sales/monthly-revenue.md)を実行し、その receipt
   を数字と一緒に保管する。数字の固定は台帳の仕事で、クエリの仕事では
   ない。
3. **月は UTC の暦月で切る。** 店は US にあり、倉庫のタイムスタンプは
   UTC である。Asia/Tokyo に直してから切ると、月末の注文が隣の月に落ちて、
   他の誰の数字とも合わなくなる。

規則 2 はこのデータセットで特に効く。`status` に履歴が無いので、receipt
を残さなかった月次は後から再現できない
([thelook_ecommerce データセット](/references/thelook-dataset.md))。
規則の見直しは年度ごとに行い、変わったときは[売上](/metrics/revenue.md)
を書き直す。再検証ではなく書き直しになる。
