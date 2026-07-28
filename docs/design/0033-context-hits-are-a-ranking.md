# ochakai 設計ドキュメント 0033: context の `hits` は順位に徹する

Status: Accepted(2026-07-26)。[0046](0046-bundle-address-space.md) §3.10 が
`hits` の行に trust tier を足す(順位に徹するという決定は不変)
Date: 2026-07-26

## 1. 背景

`get_context` / `GET /api/v1/context` は「データの質問に答える前の 1 コール」で
ある。バイト予算(2026-07-21 の PR #114、#125、#137)は、入りきらない
エントリを本文ごと配るのではなく `outline` に名前だけ出す仕組みで、
エージェントのコンテキスト窓という希少資源を守るために入った。

この予算の決定は設計ドキュメントに書かれないまま、openapi の記述と
コードのコメントだけがランドした。本ドキュメントはその決定を記録し、
同時に**予算が応答全体を縛っていなかった**という積み残しを閉じる。

## 2. 問題: 予算が応答の一部しか縛っていなかった

`ContextResult.Hits` は `[]domain.SearchHit` で、`SearchHit` は
`Knowledge` を埋め込んでいる — 本文も attrs も丸ごとである。一方
`packWithinBudget` が数えていたのは `Entries` と `Outline` だけだった。

帰結は 3 つある。

1. **応答が二重になる。** 上位エントリは `entries` と `hits` の両方に
   全文で載る。予算を指定しない呼び出しでも、常に払っている税である。
2. **予算が意味を失う。** `hits` は検索の上位 2×limit 件を持つので、
   `entries` から外して `outline` に落としたエントリが、`hits` 経由で
   全文のまま届く。呼び出し側は「取りに行け」と言われた知識を既に
   受け取っている。
3. **一番大きいものが素通りする。** `packWithinBudget` が存在する理由
   そのもの(巨大な attrs を持つエントリ)が、予算の前を素通りする。

MCP 側は PR #125 でこれに気づき、`contextRank` というローカルな射影で
`hits` を順位だけにした。REST と CLI(`--json`)は元のままだったので、
**同じ機能が面によって別の意味を持つ**状態が残った — 0015 が禁じている
形である。しかも 0015 §3.2 は「埋め込みホストは REST を使え」と誘導して
いるので、予算が効かないのは埋め込みホストの側だった。

## 3. 決定

**`hits` は全サーフェスで順位に徹する。**

### 3.1 型

`domain.ContextRank`(`id` / `type` / `title` / `status` / `score`)を
共有の wire 型として置き、`service.ContextResult.Hits` の型にする。
MCP のローカル `contextRank` は削除し、この型を使う。openapi にも
`ContextRank` スキーマを足す。

`title` は表示名(0022 の `DisplayTitle`)である。順位はポインタであり、
ポインタは fetch するかどうかを判断できるだけの情報を持つ必要がある —
id だけでは、呼び出し側は全部取りに行くか全部捨てるかしかできない。

### 3.2 `hits` は予算の外に置く

予算が縛るのは**知識**である: 全文で配る `entries` と、名前を出す
`outline`。`hits` は本文を持たず、件数も 2×limit で上限があるので、
予算の外に置いても応答が膨らまない。openapi の記述もこの通りに直す
(「応答のバイト数を縛る」→「応答に載る知識のバイト数を縛る。他に
本文を運ぶフィールドは無い」)。

### 3.3 検索の `hits` は変えない

`GET /api/v1/knowledge` / `search_knowledge` の hits は従来どおり
`SearchHit`(全文)である。**そこではエントリが答えそのもの**であって、
同じ応答に載る別のフィールドの複製ではない。区別はこの一点にある。

## 4. 壊れるもの

**REST の `/api/v1/context` の応答形が変わる。** `hits[]` の要素から
`body` / `attrs` / `links` / provenance 各フィールドが消え、
`id` / `type` / `title` / `status` / `score` だけになる。

- `hits` から本文を読んでいたクライアントは `entries` を読むように直す
  必要がある。ただし `entries` は同じ応答に既に載っており、
  `hits` にしか無いエントリ(順位が pack の足切りより下)は、
  もともと `outline` にも出ない「取りに行く候補」である。
- CLI(`ochakai context`)の表示は変わらない。「Also relevant」の行は
  `ContextRank` の title / status で従来どおり書ける。
- MCP の `get_context` は変わらない(#125 で既にこの形)。
- Web UI は `/context` を呼ばない(agent 向けの面である)。

0.13.0 の破壊的変更の 1 つとして扱う。

## 5. 関連

- [0015](0015-surface-consistency.md) — 同じ機能が面によって別の意味を
  持ってはならない、という規律。この積み残しはその違反だった。
- [0025](0025-closing-the-loop.md) — `outline` が誘導する
  `get_knowledge` / `report_outcome` の書き戻しループ。
