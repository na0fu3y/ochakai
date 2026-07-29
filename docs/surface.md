# ochakai の表面

ochakai を使う人が払うのは実装の行数ではなく**表面**である — 呼べる
エンドポイント、エージェントのコンテキストを消費するツールスキーマ、
覚えなければならないコマンド。この文書はその全部を一箇所で数える。

数えるだけで、止めはしない。止めるのは人で、この文書がするのは
**表面が増えたことを diff に出す**ことである。エンドポイントを一本足す
変更は [api/openapi.yaml](../api/openapi.yaml) の数十行の差分に埋もれる
が、ここでは `## REST (19)` が `(20)` に変わる一行として出る。
`cmd/ochakai/surface_test.go` が下の三つの節を実物と突き合わせ、
食い違えば CI が落ちる([0035](design/0035-verifiability.md):
規約を信じるのではなく、外から不変条件を読む)。

## 足す前に答える三つの問い

**既定の答えは no である。** 表面を増やす PR は、説明でこの三つに答える。

1. **誰が、何をしていて詰まったか。** 実際に起きた一件を書く。「あると
   便利」「対称性のため」「他のツールにはある」は理由ではない。
   [ROADMAP](../ROADMAP.md) が断ってきたものは、どれも使われた結果では
   なく想像から始まった機能である。
2. **既存の面で回避できるか。** 回避できるなら足さない。サーバ側の一括
   import を断った理由がこれで、既存のエンドポイントのループで足りる
   ものに二つ目の経路を作ると、買えるのは capability ではなく
   convenience だけだった([0015 §3.2](design/0015-surface-consistency.md))。
3. **何が畳めるか。** 一つ足すとき、既存のどれが要らなくなるかを探す。
   何も畳めないなら、表面が単調に増えてよい理由を書く — 書けるなら
   足してよい。書けないと気づくことが、この問いの目的である。

面ごとの既定は [0015 §3.1](design/0015-surface-consistency.md) が決めて
いる。**REST** は唯一の契約で、すべては `/api/v1` に載るか、どこにも
載らない。**CLI** は完全性の面なので既定は yes。**MCP の既定は no** —
ツールスキーマはエージェントのコンテキストから支払われるので、ツール数
は予算である。**Web UI** は人の curation に要るときだけで、BI ツールで
はない。

## この文書は番号を取らない

設計ドキュメントの番号は取らない。[0048 §3](design/0048-decision-records-for-wire-contracts.md)
が「以後、プロセスについての決定は書かない」と定めており、これはその
規則の下で動く運用であって、規則の変更ではない。増やしたくないものを
数える仕組みが、自分の数え先を増やすのでは筋が通らない。

## REST (19)

- `DELETE /api/v1/bundle/{path}`
- `DELETE /api/v1/knowledge/{id}`
- `DELETE /api/v1/reject/{id}`
- `GET /api/v1/backlinks/{id}`
- `GET /api/v1/bundle/{path}`
- `GET /api/v1/context`
- `GET /api/v1/export`
- `GET /api/v1/knowledge`
- `GET /api/v1/knowledge/{id}`
- `GET /api/v1/queues`
- `GET /api/v1/stats`
- `GET /api/v1/usage/{id}`
- `POST /api/v1/move`
- `POST /api/v1/reembed`
- `POST /api/v1/reject/{id}`
- `POST /api/v1/usage/{id}`
- `POST /api/v1/verify/{id}`
- `PUT /api/v1/bundle/{path}`
- `PUT /api/v1/knowledge/{id}`

## MCP (8)

- `delete_knowledge`
- `get_attachment`
- `get_context`
- `get_knowledge`
- `get_knowledge_usage`
- `put_knowledge`
- `report_outcome`
- `search_knowledge`

## CLI (28)

- `ochakai attach`
- `ochakai backlinks`
- `ochakai browse`
- `ochakai completion`
- `ochakai context`
- `ochakai create`
- `ochakai delete`
- `ochakai detach`
- `ochakai export`
- `ochakai get`
- `ochakai import`
- `ochakai log`
- `ochakai mcp-stdio`
- `ochakai move`
- `ochakai purge`
- `ochakai queues`
- `ochakai reembed`
- `ochakai reject`
- `ochakai report`
- `ochakai revisions`
- `ochakai search`
- `ochakai stats`
- `ochakai ui`
- `ochakai update`
- `ochakai usage`
- `ochakai use`
- `ochakai verify`
- `ochakai whoami`

## 数えていないもの

- **Web UI。** 自分のアドレス空間を持たず、`/api/v1` の client として
  動く([0006](design/0006-web-ui-serving.md))ので、増えるとすれば REST
  の節に出る。ページ自身の予算は「ビルドステップなし・フレームワーク
  なし・CDN なし」の一枚という形の制約で、数ではない。
- **`serve` / `serve-ui` / `version` / `help`。** バイナリの動かし方で
  あって、ochakai が知っていることではない。
- **実装の行数、内部パッケージ、依存。** 外から見えないものは、増えても
  使う人は払わない。困るのは維持者だけで、それは別の問題である。

## この仕組みが持てないもの

テストが読めるのは数だけで、**足したものがその場所に値したかは読め
ない**。三つの問いに答えたふりをして節を書き換えるのは何秒もかからず、
CI は通る。この仕組みが買っているのは、それを**気づかないままやれなく
する**ことだけである。判断はレビューに残り、理由は PR に残る。
