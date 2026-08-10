---
type: Insight
title: MCP の代金は本数ではなくバイトである — 17 本が 6 本より安い実例
description: semantica(17 ツール 7,771B)と ochakai(6 ツール 11,850B)の実測比較が 0076 の判断を裏返しから確認した
tags: [dogfood, mcp, positioning]
status: draft
sources:
  - id: semantica
    resource: https://github.com/semantica-agi/semantica
    title: 比較対象(mcp/schemas.py + mcp/tools から静的算出)
  - id: surface
    resource: docs/surface.md
    title: MCP の節(MCP-BYTES の定義)
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T02:37:12Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

類似 OSS の semantica と MCP 面を実測比較したところ、**ツール 17 本の
semantica(7,771 バイト、instructions なし)は、6 本の ochakai
(11,850 バイト、instructions 1,021 込み)より軽かった**。本数の少なさは
コンテキスト代金の少なさを意味しない — 0076 が「支払っているのは本数では
なくバイト」と見抜いたことの、逆向きからの確認である。

差の中身は説明の厚さである。semantica の `extract_entities` は 263 バイト
で「text を渡せ」しか言わない。ochakai の `put_concept` は一本で 3,836
バイト、OKF の書き方と型ガイドをスキーマ内で教える。つまりこれは
トレードオフの選択であって優劣ではない — **ochakai は「ツールが教える」
をバイトで買っている**。その買い物が正しいかは、説明を薄くしたときに
エージェントの書き込み失敗率が上がるかで決まり、それを測る計器が
report_outcome(C7)である。

比較の副産物: 本数だけを見て「semantica の方が表面が大きい」と主張する
と、この一点では逆転されている。positioning の議論で MCP に触れるときは
バイトで言うこと。
