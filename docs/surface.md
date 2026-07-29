# ochakai の表面

ochakai を使う人が払うのは実装の行数ではなく**表面**である — 呼べる
エンドポイント、エージェントのコンテキストを消費するツールスキーマ、
覚えなければならないコマンド、設定を間違えられる環境変数。この文書は
その全部を一箇所で数え、**何のための表面か**を先に決めている。

数えるだけで、止めはしない。止めるのは人で、この文書がするのは
**表面が増えたことを diff に出す**ことである。エンドポイントを一本足す
変更は [api/openapi.yaml](../api/openapi.yaml) の数十行の差分に埋もれる
が、ここでは `## REST (19)` が `(20)` に変わる一行として出る。
`cmd/ochakai/surface_test.go` が下の四つの節を実物と突き合わせ、
食い違えば CI が落ちる([0035](design/0035-verifiability.md):
規約を信じるのではなく、外から不変条件を読む)。

## 七つの条件

数える前に、**何のための表面かを決めておく**。ochakai が満たそうと
しているのは次の七つで、これで全部である。

| id | 条件 |
|---|---|
| C1 | 資産は利用者のもの — 丸ごと出て、丸ごと戻り、求められれば消える([0009](design/0009-provenance-portability.md)・[0031](design/0031-purge.md)・[0046](design/0046-bundle-address-space.md)) |
| C2 | Google Cloud、secret なし — Cloud Run IAM と Cloud SQL IAM で、トークンもパスワードも置かない([0002](design/0002-authn-authz.md)・[0003](design/0003-gcp-only.md)) |
| C3 | 形式は Open Knowledge Format v0.2 — 保存もワイヤも往復も OKF で、その横に第二の形式を発明しない([0036](design/0036-okf-schema-first.md)・[0046](design/0046-bundle-address-space.md)) |
| C4 | No FDE — デプロイは自分でできて、必要な操作には自分で打てるコマンドがある([0004](design/0004-cli.md)・[0007](design/0007-api-only-cli.md)) |
| C5 | Claude Code から使える — MCP over HTTP と、それを話せないクライアントのための stdio 橋([0015](design/0015-surface-consistency.md)・[0039](design/0039-mcp-stdio-bridge.md)) |
| C6 | 利用者が自分の Web サービスに埋められる小さな REST API — OpenAPI 一枚で、クライアントライブラリを要らなくする([0001](design/0001-architecture.md)・[0015](design/0015-surface-consistency.md)) |
| C7 | 人間の改善ループが測れる — 検証・結果報告・キューの長さ・答えの無かった問いを、推測ではなく数で持つ([0025](design/0025-closing-the-loop.md)・[0029](design/0029-usage-recording-off-the-read-path.md)・[0049](design/0049-queue-counts.md)・[0051](design/0051-instance-metrics-and-search-misses.md)) |

**どれにも当たらない提案は、三つの問いに進むまでもなく no である。**
逆は成り立たない — 条件に当たることは必要条件であって十分条件では
ない。「C7 に当たる」は足す理由にならず(C7 に当たる機構は無限にある)、
落ちなかったものが次の三つの問いに進む。

七つは互いに独立ではない(C4 は C2 の secret-zero に支えられ、C5 と C6
は同じナレッジを別の口から出す)。それでよい — これは分類ではなく、
**「足さない」と言うための共通の物差し**である。八つめを足すのは製品を
別のものにする決定であり、表面を一つ足すのとは桁が違う。ここに行を
足す PR は、そう扱われる。

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

## ENV (16)

環境変数も表面である — デプロイする人が読み、間違えられる。No FDE(C4)
を掲げる以上、**設定の数は「自分で立ち上げられるか」に直接効く**。
非テストの Go ソースから読み戻すので、`os.Getenv` を一つ足せばここも
動く。

- `OCHAKAI_DATABASE_URL`
- `OCHAKAI_DB_IAM_AUTH`
- `OCHAKAI_DELEGATING_CALLERS`
- `OCHAKAI_EMBEDDINGS`
- `OCHAKAI_EMBEDDING_DIM`
- `OCHAKAI_GCS_BUCKET`
- `OCHAKAI_IAP_AUDIENCE`
- `OCHAKAI_INSECURE_DEV`
- `OCHAKAI_PRODUCER`
- `OCHAKAI_PUBLIC_READ_ONLY`
- `OCHAKAI_READ_ONLY`
- `OCHAKAI_RECORD_MISSES`
- `OCHAKAI_URL`
- `OCHAKAI_VERTEX_LOCATION`
- `OCHAKAI_VERTEX_MODEL`
- `OCHAKAI_VERTEX_PROJECT`

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
