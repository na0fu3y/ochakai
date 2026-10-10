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

## 三つの層

数える前に、**何のための表面かを決めておく**。ochakai の条件は、強さの
違う三つの層に分かれる。性質は ochakai が何であるかを定め、破らない。
目的は ochakai が何のためにあるかを定め、届いたかどうかを数で確かめる。
手段は性質と目的をいまどう実現しているかであり、目的と比べて入れ替えて
よい。層を分けるのは、衝突したときにどちらを取るかを決めるためである
([0158](decisions/0158-conditions-are-properties-goals-and-means.md))。

### 性質 — 破らない

| id | 性質 |
|---|---|
| P1 | 配る内容を変えるのは裁定だけで、LLM は裁定しない。裁定は人の verify と却下、または確認ジョブが `process:` として残す確認(`machine-confirmed` と読まれる)であり、誰が裁定したかは台帳に残る([architecture](spec/architecture.md)・[loop](spec/loop.md)) |
| P2 | 資産は利用者のもの — 丸ごと出て、丸ごと戻り、求められれば消える([0009](design/0009-provenance-portability.md)・[0031](design/0031-purge.md)・[0075](design/0075-the-bundle-is-the-address-space.md)) |
| P3 | 形式は Open Knowledge Format — 保存もワイヤも往復も OKF で、その横に第二の形式を発明しない([0075](design/0075-the-bundle-is-the-address-space.md))。**「v0.2」は一つの文書を指さない**ので、留めるのは版ではなく**コミット**である(`open-knowledge-format@0b87c52`、[compatibility.md](compatibility.md) が正典): v0.2 は公開後に normative な規則を版番号を動かさずに変えており、綴りをどちらに合わせるかは [0139](design/0139-ochakai-does-not-choose-a-spelling.md)。他の形式の定義を読んで draft に写すことは第二の形式ではない — 保存とワイヤと往復が OKF のままだからである |
| P4 | 問いは、問うた本人の権限を超えない。ochakai はウェアハウスの資格情報を常設しない([architecture](spec/architecture.md)) |
| P5 | 運用者が保管し更新する長期の secret を、ochakai は持たない([identity](spec/identity.md)) |

性質を変える提案は、表面を一つ足す提案とは別の種類の決定であり、そう扱う。

### 目的 — 届いたかを数で確かめる

| id | 目的 |
|---|---|
| G1 | **No FDE** — チームが ochakai の外の人の手を借りずに、最初の**人が確かめた答え**に届く。導入はその途中の一段にすぎない。届くまでの手作業の数と時間で確かめる。いまの値は下の「最初の確かめられた答えまで」にある。**デプロイ自身のデータエージェントを off のままでも届き、入れたデプロイでは、より少ない手数で届く** — エージェントは人の裁定を安くするためにだけ使い、裁定はしない([0142](design/0142-ochakai-carries-a-data-agent-that-does-not-rule.md)・[ROADMAP](../ROADMAP.md)) |
| G2 | Claude Code から使える — MCP over HTTP と、それを話せないクライアントのための stdio 橋([0145](design/0145-four-faces-and-an-answer-that-shows-its-result.md) §3) |
| G3 | 利用者が自分の Web サービスに埋められる小さな REST API — OpenAPI 一枚で、クライアントライブラリを要らなくする([0142](design/0142-ochakai-carries-a-data-agent-that-does-not-rule.md)・[0145](design/0145-four-faces-and-an-answer-that-shows-its-result.md) §1) |
| G4 | 人間の改善ループが測れる — 検証・結果報告・キューの長さ・答えの無かった問いを、推測ではなく数で持つ([0141](design/0141-a-miss-is-read-off-the-words.md)) |
| G5 | 日本語話者にとって、類似サービスと比較したときの最適な選択肢の一つであること — 二文字の日本語語が索引で引け(移行 `0036`)、書き手が与えた別名も索引に入り([0105](design/0105-a-concept-answers-to-its-other-names.md))、埋め込みとエージェントのモデルをデプロイのリージョンに留めることが変数一つで書ける — **既定では留まらない**: 新しいベースの埋め込みは `global`、前からあるベースはリージョンで([0147](design/0147-search-and-the-default-a-base-was-made-with.md) §1.2)、エージェントの推奨モデルは `global` にしか無い([configuration.md](configuration.md)) |

### 手段 — 目的と比べて入れ替えてよい

| id | 手段 | 何を実現しているか |
|---|---|---|
| M1 | Google Cloud を前提にする(Cloud Run IAM・Cloud SQL IAM)。撤回の条件は [0115](design/0115-the-second-footing-waits-for-search.md) | P5 を設定無しで満たす。OIDC 発行者を名指したデプロイは Google Cloud の外でも P5 を満たし([0086](design/0086-a-second-way-to-say-who-is-calling.md))、そこではデータベースの資格情報が運用者の仕事に戻る |
| M2 | サーバーは SQL を実行しない | P4 |
| M3 | デプロイ自身のデータエージェントは既定 off | 課金とデータの送り先(G5 のリージョン)について、運用者が自分で決めてから入れる |
| M4 | コネクタと取り込みパイプラインを持たない | P1 — 集めたものが裁定無しに配られない |
| M5 | MCP の既定は no で、`MCP-BYTES` に天井を置く | エージェントのコンテキストの消費(G2) |
| M6 | 面ごとの上限(下の「上限」節) | 表面を増やすことを、声に出した決定にする |

### 衝突したときの規則

1. **性質は破らない。**
2. **目的と手段が衝突したら、手段を保つ側が理由を書く。** 書くのは、その
   手段がどの性質を実現しているかと、同じ性質をより少ない手数で満たす
   別の手段が無いかである。どちらも示せなければ目的を取る。手段が性質に
   結びついていても、同じ性質を満たす別の手段があれば入れ替えてよい —
   P5 を M1 と OIDC の検証の二つが満たしているのがその例である。
3. **目的どうしが衝突したら、G1 を先にする。** ほかの目的のあいだに順位は
   置かない。順位を全部に付けると、衝突のたびに表を引くだけの判断になる。
4. **どの性質にも目的にも当たらない提案は、三つの問いに進むまでもなく
   no である。** 逆は成り立たない — 当たることは必要条件であって十分条件
   ではない。「G4 に当たる」は足す理由にならず(G4 に当たる機構は無限に
   ある)、落ちなかったものが次の三つの問いに進む。

性質や目的を足すことは製品を別のものにする決定であり、表面を一つ足すの
とは桁が違う。ここに行を足す PR は、そう扱われる。

### 旧 id

2026-10 より前の記録・CHANGELOG・決定ログは、性質と目的を区別しない
八つの条件 C1〜C8 で書かれている。対応は一対一である。

| 旧 | C1 | C2 | C3 | C4 | C5 | C6 | C7 | C8 |
|---|---|---|---|---|---|---|---|---|
| 新 | P2 | P5 | P3 | G1 | G2 | G3 | G4 | G5 |

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

## 最初の確かめられた答えまで

G1 の数である。チームの誰かが、人が確かめた concept を根拠にした答えを
初めて受け取るまでに、誰が何を手でするかを数える。**ここにある段は文書
(README・`deploy/terraform`・`deploy/cloudrun`)を読んで数えたもので、
誰かが通しで歩いた記録ではない。** 時間はまだ測っていない。CI はこの節を
読まないので、手順を変える PR が、ここも書き直す。

段は、それだけで一つの判断か一つの待ちになる手作業を一つと数える。
一つの段に、続けて打つコマンドが複数入ることがある(スキーマの bootstrap
は四つのコマンドで一段である)。

### 経路 1: エージェントを入れ、問う人はブラウザだけ

運用者が 8 段、問う人が 5 段、合わせて 13 段である。

1. 運用者が `gcloud`・`terraform`・`cloud-sql-proxy`・`psql` を揃え、
   `gcloud` で認証する。Terraform が使う資格情報の用意は、Terraform の
   README に書かれていない。
2. リポジトリを取得し、`terraform.tfvars` を書く(必須の三つ、
   `invoker_members`、`enable_webui` と `webui_iap_members`、`agent_model`、
   `agent_bigquery_project`)。
3. `terraform init`・`plan`・`apply`。Cloud SQL の作成に 10〜15 分待つ。
4. スキーマを bootstrap する: `postgres` に一時的なパスワードを設定し、
   `cloud-sql-proxy` を張り、`psql` に SQL を貼り、パスワードを捨てる値に
   変える。
5. コンソールで OAuth の同意画面を作る。
6. コンソールでウェブクライアントを作り、serve-ui のオリジンとリダイレクト
   URI を登録する。serve-ui の URL は 3 が終わるまで決まらない。
7. `agent_oauth_client_id` を `terraform.tfvars` に書き、もう一度 `apply`
   する。
8. serve-ui の URL を問う人に渡す。
9. 問う人が serve-ui を開き、IAP で Google にサインインする。
10. 「BigQuery から取り込む」に `project.dataset` を書き、初回は Google の
    同意画面を通す。テーブルごとの draft ができる。
11. draft を一つ選び、説明を書く(エージェントが提案した説明を採ってもよい)。
12. ✓ で確かめる。
13. エージェントに問い、提案された SQL の実行に、会話ごとに一度同意する。

### 経路 2: エージェントを入れず、問う人が Claude Code を持つ

運用者が 5 段、問う人が 7 段、合わせて 12 段である。

1. 運用者が経路 1 の 1 を済ませる。
2. `terraform.tfvars` を書く(必須の三つと `invoker_members`)。
3. `terraform init`・`plan`・`apply`。
4. スキーマを bootstrap する(経路 1 の 4 と同じ)。
5. サービスの URL を問う人に渡す。
6. 問う人が `gcloud` を入れ、`gcloud auth login` する。
7. ochakai の CLI を入れ、`ochakai use <URL>` する。
8. `claude mcp add ochakai -- ochakai mcp-stdio` で Claude Code に繋ぐ。
9. `bq query` でスキーマを読み、`ochakai seed - | ochakai import -` に流す。
10. draft を一つ選び、説明を書いて保存する(手元の `ochakai ui` でもよい)。
11. `ochakai verify <id>` で確かめる。
12. Claude Code に問う。

### 経路 3: エージェントを入れず、問う人はブラウザだけ

この組み合わせで問う人が答えに届く道は、Claude のコネクタ
(`deploy/cloudrun` の §5e)だけである。経路 2 の運用者の段に加えて、
OAuth クライアントを作り、公開到達の MCP サービスを別に立て、Claude の
組織のオーナーがコネクタを足す。client secret は ochakai には置かないが、
Claude の組織設定に置かれる。draft を作って確かめる場所としては、
経路 1 の 5・6 と serve-ui も要る。

### この数から分かること

- 三つの経路のどれでも、運用者の段のうち 1・3・4 は同じで、どれも
  シェルと Google Cloud の権限を要る。[ROADMAP](../ROADMAP.md) の *Next* の
  一つ目(エージェントが実行できるデプロイの手順書)が縮める対象である。
- 経路 1 には、コンソールでしか作れないものが二つある(5・6)。そのうえ
  serve-ui の URL が決まるまで作れないので、`apply` が二回になる。
- 裁定は一件ずつである(経路 1 の 12、経路 2 の 11)。最初の答えに要るのは
  一件だが、使える量のベースになるまでの手数はここで決まる。*Next* の
  二つ目(一括の下書きと一括の裁定)が縮める対象である。
- エージェントを入れたデプロイ(経路 1)は、入れないデプロイ(経路 2)より
  段が一つ多い。G1 は、入れたデプロイの方が少ない手数で届くことを求めて
  いる。違いは、問う人の側の手作業が運用者の側に移ることにある(問う人の
  段は 7 から 5 になり、運用者の段は 5 から 8 になる)。

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
除く。No FDE(G1)を掲げる以上、**設定の数は「自分で立ち上げられるか」に
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
