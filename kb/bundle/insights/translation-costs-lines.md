---
type: Insight
title: 翻訳は行数を返さない
description: 日本語化で DOC-LINES がどう動くかの実測と、ページの外に出る二つの代金
tags: [dogfood, docs]
status: draft
sources:
  - id: contributing
    resource: CONTRIBUTING.md
    title: Translating a manual page の節
  - id: surface
    resource: docs/surface.md
    title: DOC の節の実測
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T02:37:12Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

マニュアルの日本語化(C8)で分かった、直感に反する三つ:

- **行数は増える。** 日本語は語間に空白が無いので一行が多くを運ぶと
  見込んでいたが、全角文字は同じ 70 桁ラップに半分しか入らない。
  loop.md 114 → 120、configuration.md 75 → 84。減るのは一行要約を並べる
  ナビゲーションページだけ(docs/README.md 101 → 96)。**行数を返すのは
  翻訳ではなく畳み込みだけである。**
- **代金はページの外にも出る。** 見出しを訳すとアンカーが動き、死んだ
  フラグメントは「正しいページの先頭に着く普通のリンク」に化けて
  レビューを生き延びる。訳した見出しの上に `<a id="…"></a>` を明示で
  置く(`TestManualLinksResolve` が落とす)。被リンクの多いページほど
  `(Japanese)` 印の付け外しが波及する。
- **訳す前に英語を疑う。** 古びた記述は「忠実に訳して」から別 PR で
  直す — 訳と修正を混ぜると、どちらの変更かレビューで読めなくなる。
