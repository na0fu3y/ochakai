---
type: Policy
resource: https://wiki.example.co.jp/finance/revenue-recognition
title: 売上計上ポリシー (FY2026)
description: どの注文を、いつ売上として数えるか、そして何をあえて引かないか
tags: [finance, revenue, policy]
generated: { by: human:sato@example.co.jp, at: 2026-07-16T06:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:05:00Z }
status: stable
---

数字が従っている規則である。この concept の resource が指す経理の wiki
ページの写しで、正であるのは向こうのページ。ここにあるのは、
[売上](/metrics/revenue.md)を読んでいるエージェントが、ナレッジベースを
出ずに規則を見られるようにするためである。

FY2026 について、あえてそう決めた三つ:

1. **注文が完了した時点で売上になる。** 完了とは
   [完了した注文](/glossary/completed-order.md)の意味、つまり出荷と入金
   確定である。受注時点でも、着荷確認時点でもない。
2. **返金は、戻ってきた月から引かない。** 一部返品にいたっては引かない。
   返品は別に追い、率として報告する。相殺はしない。分析側の売上が締めた
   数字より 1〜2% 高く出るのはこれが理由である。
3. **分析は受注日で束ね、経理の締めは入金日で束ねる。** どちらも目的に
   対して正しく、月末では二つは合わない。社外に出る数字は締めのほうで
   ある。

規則 2 は毎年度見直され、変わる可能性がいちばん高い。変わったときは
[月次売上](/queries/sales/monthly-revenue.md)に新しい `WHERE` 句が要り、
[売上](/metrics/revenue.md)は書き直しになる — 再検証ではなく、書き直しで
ある。
