# ochakai の表面

ochakai を使う人が払うのは実装の行数ではなく**表面**である — 呼べる
エンドポイント、そこに渡すパラメータとヘッダ、エージェントのコンテキスト
を消費するツールスキーマ、覚えなければならないコマンド、設定を間違え
られる環境変数。この文書はその全部を一箇所で数え、**何のための表面か**
を先に決めている。

数を出すだけでなく、上限も置く。**表面が増えたことを diff に出す**のが
下の七つの節で、**増やすと決めたことを diff に出す**のが「上限」の節で
ある。エンドポイントを一本足す変更は
[api/openapi.yaml](../api/openapi.yaml) の数十行の差分に埋もれるが、
ここでは `## REST (14)` が `(15)` に変わる一行として出て、さらに上限の
行を書き換えないと CI が通らない。
`cmd/ochakai/surface_test.go` が七つの節を実物と突き合わせ、
食い違えば落ちる([0035](design/0035-verifiability.md):
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

## 上限

下の節が数えるのは**今の大きさ**で、それがいくつであっても、文書と
ビルドが一致してさえいれば通る。数は体温計であって、温度調節器では
ない。ここがその調節器である — 面ごとに天井を宣言し、超えたら CI が
落ちる。

- REST: 11
- PARAM: 18
- HEADER: 9
- MCP: 8
- CLI: 25
- ENV: 16
- VOCAB: 36

**天井は一つのファイルの一行で、誰でも上げられる。それは弱点ではなく
狙いである。** この仕組みが買っているのは、上の節と同じく、**黙って
やれなくする**ことだけである。上限を動かす PR は「ochakai を大きく
する」と声に出して言っている PR であり、三つの問いはそのときのために
ある。

**下げるのは日常、上げるのは決定。** 畳み込みで数が減ったら、上限も
一緒に下げる — さもないと次の追加が無言で通る余白が貯まる。減った数の
横に古い天井が残っていれば、それは diff に見えている。

## この文書は番号を取らない

設計ドキュメントの番号は取らない。[0048 §3](design/0048-decision-records-for-wire-contracts.md)
が「以後、プロセスについての決定は書かない」と定めており、これはその
規則の下で動く運用であって、規則の変更ではない。増やしたくないものを
数える仕組みが、自分の数え先を増やすのでは筋が通らない。

## REST (11)

- `DELETE /api/v1/bundle/{path}`
- `GET /api/v1/bundle/{path}`
- `GET /api/v1/context`
- `GET /api/v1/search`
- `GET /api/v1/stats`
- `GET /api/v1/usage/{id}`
- `POST /api/v1/move`
- `POST /api/v1/reembed`
- `POST /api/v1/review/{id}`
- `POST /api/v1/usage/{id}`
- `PUT /api/v1/bundle/{path}`

19 → 14 → 11。前半は
[0046 §3.5](design/0046-bundle-address-space.md) の畳み込みで、消えた
のは面であって能力ではなかった — concept の第二の住所
(`/api/v1/knowledge/{id}` 3 操作)、`/api/v1/backlinks/{id}`(検索の
`links_to=` へ)、`/api/v1/export`(バンドルのパスに
`Accept: application/gzip` で、ルートが全体)。一覧は `/api/v1/search`
に改名した。

後半は [0055](design/0055-one-ruling-one-face.md) の 3 操作で、こちらも
能力は落ちていない。`GET /api/v1/queues` は `stats` が同じキーで既に
返していた 3 つの数の第二の住所で、唯一できたことが部分木への絞り込み
だったので、**絞り込みのほうを `stats` に移した** — 結果として、共有
デプロイのチームは自分のキューだけでなく自分の知識も数えられるように
なった(§3.2)。`verify` / `reject` / `reject の DELETE` は
`POST /api/v1/review/{id}` 一本になった。三つは**構造的性質を全て共有
していた** — 台帳に追記する、文書と status と ETag を動かさない、人の
面に載り MCP には載らない — 違うのは記録される値だけで、それは
「パラメータを持つ一つの操作」の定義である(§3.4)。

`report_outcome`(`POST /api/v1/usage/{id}`)はそこに畳んでいない。
人の裁定ではなく機械の観測であり、[0015 §3.4](design/0015-surface-consistency.md)
が Web UI に載せないと決めている当のものである。数のためだけの畳み込み
は、この文書が数えようとしているものを増やす。

## PARAM (18)

クエリパラメータも表面である。**操作の数だけを数えていた間、圧力は
ここへ逃げていた** — エンドポイントをパラメータや content type に
畳むと操作数は必ず下がり、決して上がらないので、あらゆる畳み込みが
無条件の勝ちに見えた。[0046 §3.5](design/0046-bundle-address-space.md)
の 19 → 14 は 3 本を `links_to=`・`Accept: application/gzip`・パスの
接尾辞へ移したのに、記録に残ったのは消えた 5 本だけである。

数えるのは**名前の異なり数**であって出現回数ではない。`limit` は 5 つの
操作で同じ意味を持ち、覚えるのは一度きりだから、既存の語彙を使い回す
のは無料で、**語を発明することが代金である**。これが持ちたい誘因である。

パス変数(`{path}`・`{id}`)は数えない。操作する対象の住所であって、
その横に置かれた選択肢ではない。

19 → 18 は [0058](design/0058-filters-nobody-arrived-through.md) §2.1 が
`min_score` を廃したぶんである。**畳み込みではなく削除で、能力は落ちて
いる** — この文書に並ぶ他の減り方とは種類が違う。落ちた能力は「較正した
呼び出し元がスコアの床を置く」ことで、較正の手順を製品が説明できず、
同梱の唯一の利用者が既定で切っていたものだった。

- `attachments`
- `budget`
- `cursor`
- `days`
- `fm.{key}`
- `history`
- `limit`
- `links_to`
- `prefix`
- `purge`
- `q`
- `rejected`
- `sort`
- `source`
- `status`
- `tag`
- `trust`
- `type`

## HEADER (9)

表面が隠れる三つ目の場所。`Ochakai-Read-Only` と `Ochakai-Unchanged` は
クライアントが実装しなければならないプロトコルで、`If-Match` は
[0030](design/0030-optimistic-locking.md) の全部を運んでいる —
どれもエンドポイントの数には現れない。リクエストとレスポンスを分けず、
**ワイヤを流れる名前**で数える。読む人が出会うのはそれだからである。

- `Content-Disposition`
- `ETag`
- `If-Match`
- `If-None-Match`
- `Ochakai-Note`
- `Ochakai-Read-Only`
- `Ochakai-Unchanged`
- `X-Ochakai-On-Behalf-Of`
- `X-Ochakai-Producer`

## MCP (8)

- `delete_concept`
- `get_attachment`
- `get_concept`
- `get_concept_usage`
- `get_context`
- `put_concept`
- `report_outcome`
- `search_concepts`

8 本のまま、[0054](design/0054-concept-is-the-okf-word.md) が 5 本を
改名した — 知識の単位は OKF SPEC §2 の語で **concept** であり、文書が
既にその語で書いていたのにツール名だけが `knowledge` を名乗っていた。
能力も引数も応答も変わらない。

## CLI (25)

- `ochakai attach`
- `ochakai browse`
- `ochakai completion`
- `ochakai context`
- `ochakai delete`
- `ochakai detach`
- `ochakai export`
- `ochakai get`
- `ochakai import`
- `ochakai log`
- `ochakai mcp-stdio`
- `ochakai move`
- `ochakai purge`
- `ochakai put`
- `ochakai reembed`
- `ochakai reject`
- `ochakai report`
- `ochakai revisions`
- `ochakai search`
- `ochakai stats`
- `ochakai ui`
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

## VOCAB (36)

七つ目の次元は**語**である。上の六つが数えているのは機構 — 呼べるもの、
設定できるもの — であって、**キュレーターが頭に入れておくもの**は一つも
数えていなかった。型の綴り、ライフサイクル値、信頼の段、一覧モード、
裁定、キューの名前、リビジョンが記録される動詞。どれもドキュメントが
教え、誰かが覚えるものなのに、増やしても上の六つは 1 も動かない。

**圧力はここへ逃げていた。** [0055](design/0055-one-ruling-one-face.md)
は REST 3 操作を 1 本に畳んだが、覚える語は 3 つのままである(操作は
1 つ、値が 3 つ)。[0054](design/0054-concept-is-the-okf-word.md) は
MCP ツール 5 本を改名したが、本数は 8 のままで、**変わったのは利用者が
知る語そのもの**だった。この文書は自分についてそう書いていた —
「数え方を一つ足すたびに、次の逃げ道が一つ見えるようになるだけである」。
これがその次の一つである。

値は `internal/domain` から読む。各面が既にそこから語彙を取っている
ので、**製品が言えるようになった瞬間に数に出る**。表記は
`族.値`(`status.draft`、`sort.failed`)で、理由は二つある — 素の値を
アルファベット順に並べても読めないこと、そして**一つの綴りが二つの族で
別の意味を持つとき、それは覚える語が 2 つある**ということ(`failed` は
一覧モードであり、同時に outcome の値でもある — 報告する行為と、一覧を
求める行為は別である)。

**逆に、一つのものが二つの名前を着ていたときは 1 と数える。**
38 → 36 は [0059](design/0059-a-queue-is-named-by-its-listing.md) が
`queue.reported_wrong` / `queue.past_expiry` を `failed` / `stale_after`
に改名したぶんで、`stats` が数える集合と `sort=` が並べる集合は**同じ
一つ**だから、キューは自分の名前を持たない。ここで減ったのは機構では
なく**覚える語そのもの**である — 数えていなければ、改名は「本数が動か
ない変更」として通っていた。`queue.drafts` だけが残るのは、それが単一
の sort ではないからである(`sort=usage` + `status=draft`)。

型は閉じた集合ではない([0038](design/0038-type-vocabulary-realignment.md):
一行の値ならどれでも型になる)。ここで数えているのは**製品が教える
推奨語彙**であって、書ける値の全体ではない。それでも数えるのは、
11 の綴りを `--type` のヘルプにも MCP スキーマにも
[knowledge.md](knowledge.md) の表にも書いている以上、それは利用者が
払っているものだからである。

- `change.attach`
- `change.create`
- `change.delete`
- `change.detach`
- `change.move`
- `change.reject`
- `change.update`
- `change.verify`
- `change.withdraw`
- `outcome.failed`
- `outcome.worked`
- `queue.drafts`
- `ruling.rejected`
- `ruling.verified`
- `ruling.withdrawn`
- `sort.failed`
- `sort.stale_after`
- `sort.usage`
- `sort.verified_at`
- `status.deprecated`
- `status.draft`
- `status.stable`
- `trust.human-reviewed`
- `trust.machine-confirmed`
- `trust.unverified`
- `type.API Endpoint`
- `type.Attested Computation`
- `type.BigQuery Dataset`
- `type.BigQuery Table`
- `type.Glossary Term`
- `type.Insight`
- `type.Metric`
- `type.Playbook`
- `type.Policy`
- `type.Reference`
- `type.Skill`

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

数える次元を増やしても、それは変わらない。PARAM と HEADER が塞いだのは
「操作を畳めば無条件に得」という**特定の逃げ道**であって、逃げ道一般では
ない。VOCAB が塞いだのも一つで、「機構を畳んで語を増やせば得」という
逃げ道である — `sort=` の 4 つのモードは `sort` 一語で 1 と数えられて
いたが、いま `sort.*` として 4 語に数えられる。

**そして次の逃げ道が見えている。** 4 つのモードは 4 語と数えられるように
なったが、それぞれが別の limit 既定値と別の cursor 可否を持つことは
まだどこにも数えられていない。`GET /api/v1/bundle/{path}` は Accept と
パスの接尾辞で 6 通りの別物を返して 1 と数えられる。エラーの文言も、
`ochakai://` という URI 形式も、`human:` / `process:` という actor の
綴りも数えていない。**数え方を一つ足すたびに、次の逃げ道が一つ見える
ようになるだけである。** それでよい。これは判断を置き換える装置では
なく、判断すべき瞬間を見えるようにする装置である。
