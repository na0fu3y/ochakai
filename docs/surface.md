# ochakai の表面

ochakai を使う人が払うのは実装の行数ではなく**表面**である — 呼べる
エンドポイント、そこに渡すパラメータとヘッダ、エージェントのコンテキスト
を消費するツールスキーマ、覚えなければならないコマンドとフラグ、設定を
間違えられる環境変数、覚える語、読まされるページ。この文書はその全部を
一箇所で数え、**何のための表面か**を先に決めている。

数は `cmd/ochakai/surface_test.go` がビルドと突き合わせ、食い違えば落ちる。
エンドポイントを一本足す変更は契約の数十行の差分に埋もれるが、ここでは
`## REST (18)` が `(19)` に変わる一行として出て、さらに天井の行を書き換え
ないと CI が通らない。**数がいつ・なぜ動いたかは、この文書ではなく、
動かした PR と [CHANGELOG](../CHANGELOG.md) が持つ。**

## 八つの条件

数える前に、**何のための表面かを決めておく**。ochakai が満たそうと
しているのは次の八つで、これで全部である。

| id | 条件 |
|---|---|
| C1 | 資産は利用者のもの — 丸ごと出て、丸ごと戻り、求められれば消える([0009](design/0009-provenance-portability.md)・[0031](design/0031-purge.md)・[0075](design/0075-the-bundle-is-the-address-space.md)) |
| C2 | secret を一つも置かないこと — その性質を Cloud Run IAM と Cloud SQL IAM が無設定で買う([0065](design/0065-identity-and-provenance.md)・[0003](design/0003-gcp-only.md))。**性質と買い方は同じではない**: OIDC 発行者を名指したデプロイは Google Cloud の外でも secret を増やさずに誰が呼んでいるかを答え([0086](design/0086-a-second-way-to-say-who-is-calling.md))、そこではデータベースの資格情報が運用者の仕事に戻る。**残りを撤回してよい条件は [0115](design/0115-the-second-footing-waits-for-search.md)** |
| C3 | 形式は Open Knowledge Format — 保存もワイヤも往復も OKF で、その横に第二の形式を発明しない([0075](design/0075-the-bundle-is-the-address-space.md))。**「v0.2」は一つの文書を指さない**ので、留めるのは版ではなく**コミット**である(`open-knowledge-format@0b87c52`、[compatibility.md](compatibility.md) が正典): v0.2 は公開後に normative な規則を版番号を動かさずに変えており、綴りをどちらに合わせるかは [0139](design/0139-ochakai-does-not-choose-a-spelling.md) |
| C4 | No FDE — デプロイは自分でできて、必要な操作には自分で打てるコマンドがある([0145](design/0145-four-faces-and-an-answer-that-shows-its-result.md))。**デプロイ自身のデータエージェント(既定 off)はこの条件のためにある** — チームが自前のエージェントを作らずに済み、空のベースを人の手で埋め切れない分を LLM が下書きする。LLM は人の裁定を安くするためにだけ使い、裁定はしない([0142](design/0142-ochakai-carries-a-data-agent-that-does-not-rule.md)・[ROADMAP](../ROADMAP.md)) |
| C5 | Claude Code から使える — MCP over HTTP と、それを話せないクライアントのための stdio 橋([0145](design/0145-four-faces-and-an-answer-that-shows-its-result.md) §3) |
| C6 | 利用者が自分の Web サービスに埋められる小さな REST API — OpenAPI 一枚で、クライアントライブラリを要らなくする([0142](design/0142-ochakai-carries-a-data-agent-that-does-not-rule.md)・[0145](design/0145-four-faces-and-an-answer-that-shows-its-result.md) §1) |
| C7 | 人間の改善ループが測れる — 検証・結果報告・キューの長さ・答えの無かった問いを、推測ではなく数で持つ([0141](design/0141-a-miss-is-read-off-the-words.md)) |
| C8 | 日本語話者にとって、類似サービスと比較したときの最適な選択肢の一つであること — 二文字の日本語語が索引で引け(移行 `0036`)、書き手が与えた別名も索引に入り([0105](design/0105-a-concept-answers-to-its-other-names.md))、埋め込みとエージェントのモデルをデプロイのリージョンに留めることが変数一つで書ける — **既定では留まらない**: 新しいベースの埋め込みは `global`、前からあるベースはリージョンで([0147](design/0147-search-and-the-default-a-base-was-made-with.md) §1.2)、エージェントの推奨モデルは `global` にしか無い([configuration.md](configuration.md)) |

**どれにも当たらない提案は、三つの問いに進むまでもなく no である。**
逆は成り立たない — 条件に当たることは必要条件であって十分条件では
ない。「C7 に当たる」は足す理由にならず(C7 に当たる機構は無限にある)、
落ちなかったものが次の三つの問いに進む。

八つは互いに独立ではない(C4 は C2 の secret-zero に支えられ、C5 と C6
は同じナレッジを別の口から出す)。それでよい — これは分類ではなく、
**「足さない」と言うための共通の物差し**である。九つめを足すのは製品を
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
   convenience だけだった([0067 §5.2](design/0067-four-faces-and-what-they-decline.md))。
3. **何が畳めるか。** 一つ足すとき、既存のどれが要らなくなるかを探す。
   何も畳めないなら、表面が単調に増えてよい理由を書く — 書けるなら
   足してよい。書けないと気づくことが、この問いの目的である。

面ごとの既定は [0067 §1](design/0067-four-faces-and-what-they-decline.md) が決めて
いる。**REST** は唯一の契約で、すべては `/api/v1` に載るか、どこにも
載らない。**CLI** は完全性の面なので既定は yes。**MCP の既定は no** —
ツールスキーマはエージェントのコンテキストから支払われるので、ツール数
は予算である。**Web UI** は人の curation に要るときだけで、BI ツールで
はない。

## 上限

下の節が数えるのは**今の大きさ**で、ここがその天井である — 面ごとに
宣言し、超えたら CI が落ちる。

- REST: 18
- PARAM: 21
- HEADER: 13
- MCP: 6
- MCP-BYTES: 10900
- MCP-BYTES-SLACK: 500
- CLI: 25
- FLAG: 28
- ENV: 18
- VOCAB: 46
- DOC: 27

- **天井は一行で、誰でも上げられる。** それは弱点ではなく狙いで、上げる
  PR は「ochakai を大きくする」と声に出して言っている PR になる。三つの
  問いはそのときのためにある。
- **下げるのは日常、上げるのは決定。** 名前の一覧の天井は実数と一致する
  ことを CI が確かめるので、畳み込みで数が減れば天井も下げないと落ちる。
- **`MCP-BYTES` だけは量に天井を置く。** ツールスキーマと instructions は
  エージェントが毎ターン丸ごと持つので、覚えるのではなく毎回払う。量は
  実数ちょうどに乗らないので、`MCP-BYTES-SLACK` が「天井よりどれだけ少なく
  てよいか」を宣言し、それより余白が空けば落ちる。
- **散文の量には天井を置かない。** 捕まえたい形(面を畳み、畳んだ説明を
  長く書く)より、正しい増加に鳴るほうが多かった。マニュアルが太ることへの
  歯止めは、ページの持ち主を一つに決める規則と、レビューである。

## REST (18)

`/api/v1` の操作を、[api/openapi.yaml](../api/openapi.yaml) からメソッドとパスで
数える。**REST は唯一の契約**で、すべては `/api/v1` に載るか、どこにも
載らない([spec/faces.md](spec/faces.md))。

- `DELETE /api/v1/bundle/{path}`
- `GET /api/v1/access`
- `GET /api/v1/agent/turns`
- `GET /api/v1/bundle/{path}`
- `GET /api/v1/search`
- `GET /api/v1/stats`
- `GET /api/v1/usage/{id}`
- `POST /api/v1/agent`
- `POST /api/v1/agent/turns`
- `POST /api/v1/agent/turns/{id}`
- `POST /api/v1/agent/turns/{id}/revisions/{concept}`
- `POST /api/v1/frontmatter`
- `POST /api/v1/move`
- `POST /api/v1/reembed`
- `POST /api/v1/review/{id}`
- `POST /api/v1/usage/{id}`
- `PUT /api/v1/access`
- `PUT /api/v1/bundle/{path}`

## PARAM (21)

クエリパラメータの**名前の異なり数**を数える。操作をパラメータや content
type に畳めば操作の数は必ず下がるので、ここを数えないと圧力が逃げる。
`limit` は五つの操作で同じ意味を持ち、覚えるのは一度なので、既にある語を
使い回すのは無料で、**語を発明することが代金である**。パス変数(`{path}`・
`{id}`)は対象の住所なので数えない。

- `created_by`
- `cursor`
- `days`
- `dry_run`
- `files`
- `fm.{key}`
- `history`
- `keep`
- `limit`
- `links_to`
- `note`
- `prefix`
- `purge`
- `q`
- `sort`
- `source`
- `status`
- `tag`
- `trust`
- `type`
- `verdict`

## HEADER (13)

ワイヤを流れるヘッダの**名前**を、要求と応答を分けずに数える。
`Ochakai-Read-Only` や `If-Match` はクライアントが実装するプロトコルで、
エンドポイントの数には現れないからである。応答本文への追加はここには
現れない(語が増えれば VOCAB に出る)。

- `Cache-Control`
- `Content-Disposition`
- `Content-Security-Policy`
- `ETag`
- `If-Match`
- `If-None-Match`
- `Ochakai-Note`
- `Ochakai-On-Behalf-Of`
- `Ochakai-Plan`
- `Ochakai-Producer`
- `Ochakai-Read-Only`
- `Ochakai-Unchanged`
- `X-Content-Type-Options`

## MCP (6)

実セッションの `tools/list` が返すツールを数える。**MCP の既定は no** — ツール
スキーマはエージェントのコンテキストから払われるので、本数は予算である。
本数のほかに**量**も数える(`MCP-BYTES`): `tools/list` のツール一件ぶんの
JSON を入力・出力の両方のスキーマごとそのまま測ったものと、instructions の
和である。支払っているのは本数ではなくバイトだからである。

- `get_concept`
- `get_file`
- `list_concepts`
- `put_concept`
- `report_outcome`
- `search_concepts`

## CLI (25)

利用者が打つコマンドを数える。**CLI は完全性の面**で既定は yes だが、能力が
一つならコマンドも一つである。`serve` / `serve-ui` / `version` / `help` は
バイナリの動かし方なので数えない(下の「数えていないもの」)。

- `ochakai access`
- `ochakai browse`
- `ochakai completion`
- `ochakai delete`
- `ochakai eval`
- `ochakai export`
- `ochakai get`
- `ochakai import`
- `ochakai list`
- `ochakai log`
- `ochakai mcp-stdio`
- `ochakai move`
- `ochakai purge`
- `ochakai put`
- `ochakai reembed`
- `ochakai report`
- `ochakai revisions`
- `ochakai search`
- `ochakai seed`
- `ochakai stats`
- `ochakai ui`
- `ochakai usage`
- `ochakai use`
- `ochakai verify`
- `ochakai whoami`

## FLAG (28)

フラグの**名前の異なり数**を、短いフラグも含めて数える。コマンドだけを
数えるのは、REST で操作だけを数えるのと同じ見落としで、圧力はフラグへ
逃げる。既にある語(`--json`・`--limit`・`-f`)を使い回すのは無料である。

- `created-by`
- `cursor`
- `days`
- `directory`
- `download`
- `dry-run`
- `exit-code`
- `f`
- `fm`
- `if-match`
- `json`
- `limit`
- `links-to`
- `name`
- `no-files`
- `note`
- `once`
- `only-if-new`
- `port`
- `prefix`
- `project`
- `source`
- `status`
- `strict`
- `tag`
- `trust`
- `type`
- `url`

## ENV (18)

非テストの Go ソースの `os.Getenv` の呼び出しから読み戻す — 数えるのは
呼び出しであって `OCHAKAI_` という接頭辞ではない(Cloud Run が設定する
`PORT` も入る)。OS が定義する変数(`XDG_CONFIG_HOME`・`AppData`)だけを
除く。No FDE(C4)を掲げる以上、**設定の数は「自分で立ち上げられるか」に
直接効く**。名前と値の間違いを起動時に断る規則は
[spec/deployment.md](spec/deployment.md)。

- `NO_COLOR`
- `OCHAKAI_ADMINS`
- `OCHAKAI_AGENT`
- `OCHAKAI_BIGQUERY_PROJECT`
- `OCHAKAI_DATABASE_URL`
- `OCHAKAI_DB_IAM_AUTH`
- `OCHAKAI_DELEGATING_CALLERS`
- `OCHAKAI_EMBEDDINGS`
- `OCHAKAI_GCS_BUCKET`
- `OCHAKAI_IAP_AUDIENCE`
- `OCHAKAI_MODE`
- `OCHAKAI_OAUTH_CLIENT_ID`
- `OCHAKAI_OIDC_AUDIENCE`
- `OCHAKAI_OIDC_ISSUER`
- `OCHAKAI_PRODUCER`
- `OCHAKAI_RECORD_MISSES`
- `OCHAKAI_URL`
- `PORT`

## VOCAB (46)

キュレーターが頭に入れておく**語**を、`internal/domain` から読んで数える —
型の綴り、status、trust、一覧の並び、裁定、キューの名、リビジョンの動詞、
結果報告、書き込みの plan、エラーの code。表記は `族.値` で、**一つの綴りが
二つの族で別の意味を持てば 2 と数え**(`failed` は並びでもあり結果報告でも
ある)、**一つのものが二つの名前を着ていれば 1 と数える**。機構を畳んで
語を増やす逃げ道を塞ぐ次元である。語の意味は
[spec/vocabulary.md](spec/vocabulary.md)。

- `change.add_file`
- `change.create`
- `change.delete`
- `change.move`
- `change.reject`
- `change.remove_file`
- `change.update`
- `change.verify`
- `error.already_exists`
- `error.forbidden`
- `error.internal`
- `error.invalid`
- `error.method_not_allowed`
- `error.not_deleted`
- `error.not_found`
- `error.precondition_failed`
- `error.read_only`
- `error.too_large`
- `error.unsupported`
- `outcome.failed`
- `outcome.worked`
- `plan.created`
- `plan.unchanged`
- `plan.updated`
- `queue.drafts`
- `queue.edited`
- `ruling.verified`
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
- `type.Attested Computation`
- `type.BigQuery Dataset`
- `type.BigQuery Table`
- `type.Glossary Term`
- `type.Insight`
- `type.Metric`
- `type.Policy`
- `type.Reference`
- `type.Skill`

## DOC (27)

**読まされるページ**を数える — 使う人が評価し、使い、動かすために送られる
markdown のページである。ページを一枚足せばこの数が動く。既存のページが
太ることは数えない(上限の節)。

数えないページとその理由:

- **[docs/spec](spec/README.md)・[docs/decisions](decisions/README.md)・
  [docs/design](design/README.md)。** 利用者ではなく、変えようとする人が
  読む。現行仕様は領域ごとに一本で書き換えるので、冊数は領域の数で止まる。
  決定ログは一本の長さに `DECISION-LINES` の天井があり、0001–0152 の記録は
  凍結した履歴である。記録にかけていた一冊の天井・墓標・英語要約・
  表の一行の天井は、履歴と現行を一冊に兼ねさせていたための仕組みで、
  二つを分けたときに退役した(CONTRIBUTING.md「Design: spec and
  decisions」)。
- **OKF ドキュメント。** `examples/demo` の 37 件も
  `examples/bigquery-catalog/bundle` も、プロジェクト自身のナレッジで
  ある `kb/bundle` も、ochakai が**保存するもの**であって ochakai に
  ついての説明ではない。frontmatter を持つ md は知識であり、ここでは
  数えない。
- **CHANGELOG。** 過去の台帳で、読むのは一エントリである。通して読む
  ものを数えるこの節に、追記だけで伸びるものを混ぜない。
- **CONTRIBUTING・CLAUDE.md・行動規範・`.github`・`.claude`。**
  変える人の面であって、使う人の面ではない。
- **生成された参照ページ。** ビルドから生成され、テストが往復を保証し、
  固定ヘッダー以外に手書きの散文が無いもの。いまは [docs/cli.md](cli.md)
  だけである。

- `README.md`
- `ROADMAP.md`
- `SECURITY.md`
- `SUPPORT.md`
- `deploy/cloudrun/README.md`
- `deploy/terraform/README.md`
- `docs/README.md`
- `docs/architecture.md`
- `docs/compatibility.md`
- `docs/configuration.md`
- `docs/en.md`
- `docs/faq.md`
- `docs/guides/git-review.md`
- `docs/guides/golden-query-canary.md`
- `docs/guides/mcp-clients.md`
- `docs/guides/onboarding.md`
- `docs/guides/operating.md`
- `docs/guides/rest-integration.md`
- `docs/guides/troubleshooting.md`
- `docs/loop.md`
- `docs/positioning.md`
- `docs/surface.md`
- `examples/README.md`
- `examples/bigquery-catalog/README.md`
- `examples/claude-code/CLAUDE.md`
- `examples/claude-code/README.md`
- `kb/README.md`

## 数えていないもの

- **Web UI。** 自分のアドレス空間を持たず、`/api/v1` の client として
  動く([0130](design/0130-the-web-ui-and-the-fields-of-a-document.md) §2)ので、増えるとすれば REST
  の節に出る。ページ自身の予算は「ビルドステップなし・フレームワーク
  なし・CDN なし」の一枚という形の制約で、数ではない。
- **`serve` / `serve-ui` / `version` / `help`。** バイナリの動かし方で
  あって、ochakai が知っていることではない。
- **`/.well-known/oauth-protected-resource/mcp` と `/mcp` の 401 の
  `WWW-Authenticate`**([0151](design/0151-claude-reaches-the-knowledge-through-the-persons-google-sign-in.md))。
  自分で検証するデプロイが、トークンを持たないクライアントに認証の行き先を
  教える握手で、覚える語は無く、変数も増えない。`/api/v1` の外の住所で、
  REST と HEADER の節は `api/openapi.yaml` から数えるので、そこには出ない。
- **実装の行数、内部パッケージ、依存。** 外から見えないものは、増えても
  使う人は払わない。困るのは維持者だけで、それは別の問題である。

- **`deploy/terraform` の変数。** このモジュールは Cloud Run + Cloud SQL
  という一つのホスティング経路の上に立つ任意の便宜レイヤーで、
  [deploy/terraform/README.md](../deploy/terraform/README.md) 自身が
  「gcloud ガイドは引き続きリファレンスであり正とする情報源」であり
  二つが食い違えばこちらにバグがあると宣言している —
  [0067 §1](design/0067-four-faces-and-what-they-decline.md)
  が既定を決めている REST・CLI・MCP・Web UI の四つの面のどれでもない。
  ochakai 自身に届く変数(`enable_gcs_files` → `OCHAKAI_GCS_BUCKET`、
  `read_only` / `public_read_only` → `OCHAKAI_MODE`)は、ENV が既に
  数えている一語が IaC の綴りを着ているだけであり、二度数えれば VOCAB
  が退けた「一つのものが二つの名前」を今度は次元をまたいで踏む。届かない
  変数(`region`・`image_tag`・`database_backups`・`maintenance_users`
  など)は Cloud SQL・Cloud Run・GCS 側の資源の選択であって、`serve` /
  `serve-ui` の引数を数えないのと同じ理由で ochakai が知っていることでは
  ない。読む代金は `deploy/terraform/README.md` として DOC が既に数えて
  いる — 数えていないのは HCL の変数名で、それはこのモジュールを使わない
  という選択で丸ごと避けられる代金だからである。

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

DOC が塞いだのはさらに一つで、これは他とは向きが違う —
**「表面を畳んで、畳んだ説明を書けば得」**という逃げ道である。上の八つ
だけを見ていると、面を一つ減らす PR は必ず勝ちに見える。その PR が記録と
索引と要約と段落を残していくことは、どの数にも出なかった。減った面と
増えた行が同じ表に並ぶのは、この節が足されて初めてである。

FLAG が塞いだのはもう一つで、「REST を畳んで CLI に逃がせば得」という
逃げ道である。[0075](design/0075-the-bundle-is-the-address-space.md) と
[0068](design/0068-how-a-face-is-added-and-removed.md) の畳み込みで REST と CLI
のコマンドは減ったが、**その間 CLI のフラグは一度も数えられていな
かった** — 数え始めた時点で 29 で、うち 14 が `ochakai search` 一本に
付いている。

**そして次の逃げ道が見えている。** 4 つのモードは 4 語と数えられるように
なったが、それぞれが別の limit 既定値と別の cursor 可否を持つことは
まだどこにも数えられていない。`GET /api/v1/bundle/{path}` は Accept と
パスの接尾辞で 6 通りの別物を返して 1 と数えられる。`ochakai://` という
URI 形式も、`human:` / `process:` という actor の綴りも数えていない。
エラーはここに並んでいたが、[0083](design/0083-an-error-carries-a-code.md)
が文言の隣に符号を置いたぶんだけ VOCAB に移った — 残る文言のほうは、
書き直してよいものとして今も数えていない。**数え方を一つ足すたびに、次の逃げ道が一つ見える
ようになるだけである。** それでよい。これは判断を置き換える装置では
なく、判断すべき瞬間を見えるようにする装置である。
