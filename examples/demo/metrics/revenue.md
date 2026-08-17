---
type: Metric
title: 売上
description: 完了した注文の税込金額を、受注日で束ねたもの(円)
tags: [sales, revenue, finance]
sources:
  - id: rev-policy
    resource: https://wiki.example.co.jp/finance/revenue-recognition
    title: 売上計上ポリシー (FY2026)
    author: human:tanaka@example.co.jp
    last_modified: "2026-04-01"
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-18T05:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:15:00Z }
status: stable
grain: order
synonyms: [トップライン, top line, revenue]
unit: JPY
---

売上は、[完了した注文](/glossary/completed-order.md)の `total_price` を、
その注文の作成時刻で束ねて合計したものである。定義は `SUM(total_price)`
がすべてで、この数字についての議論はすべて、どの行が入るかについての議論
である。

自明ではないことが三つある:

- **税込である。** `total_price` は消費税と送料を含む、客が払った金額で
  ある。経理の売上高は税抜きで、こちらより約 9% 低い。この二つは合うように
  できていない。
- **返金は引かない。** 全品返品された注文は完了の集合から出るが、一部返品
  の注文は出ない — [完了した注文](/glossary/completed-order.md)を見ること。
  普通の月で 1〜2% ぶん多く出る。
- **受注日で束ねる。入金日ではない。** 経理の締めは入金日を使うので、月末
  2日ぶんが二つの系で別の月に落ちる。[^rev-policy]

## 呼び名

この数字を社内で指す語が **売上** であり、この concept が定義しているのは
それである。紛らわしい語が二つあり、どちらもこの指標ではない:

- **純売上** — 経理が使う税抜きの数字。上の「税込である」のとおり、この
  指標より約 9% 低い。同じものだと思って突き合わせると必ず合わない。
- **流通総額 / GMV** — キャンセルと返品を含む受注ベースの合計。
  [完了した注文](/glossary/completed-order.md)だけを数えていないので、この
  指標とは別物である。

倉庫の側では `revenue` という列名で出てくる。表の列は英語のままなので、
この concept は両方の名前で引けるようにしてある。

検証済みの計算方法は[月次売上](/queries/sales/monthly-revenue.md)である。
動きを良し悪しで読む前に、[売上の読み方](/insights/reading-revenue.md)を
読むこと — 季節の振れ幅は、調査されるたいていの変化より大きい。月の途中で
その月を読むなら[月末の着地見込み](/insights/着地見込み.md)がある。

客が戻ってきたかどうかについて、売上は何も言わない。それは
[リピート購入率](/metrics/repeat-purchase-rate.md)で、まだ draft である。

[^rev-policy]: 売上計上ポリシー (FY2026)
