# デプロイの姿勢と環境変数

一つのデプロイが**何であるか**を一語で言い、その設定を**間違えられない**
ようにすること。No FDE(C4)の半分は、自分で立てて、自分で設定を読めることで
ある。

## 形

- **`ochakai serve`** がサーバー本体(REST・MCP)で、Cloud Run と Cloud SQL
  (PostgreSQL)に置く。データベースへは `OCHAKAI_DB_IAM_AUTH` で Cloud SQL
  IAM 認証を使い、パスワードを持たない。
- **`ochakai serve-ui`** がチーム向けの Web UI で、別の Cloud Run サービスと
  して `serve` の前に立つ(`OCHAKAI_URL` に向ける)。IAP を前に置けば本人の
  身元が通る([identity.md](identity.md))。一人で使うなら手元の
  **`ochakai ui`** で足りる。
- 推奨の立て方は `deploy/terraform` の `terraform apply` 一回と、スキーマの
  初期化の一手順で、月 10 ドル程度から。`deploy/cloudrun` はその元になった
  gcloud の手順で、両者が食い違えば後者が正である。
- 起動時にマイグレーションを流す(一方向、戻れない)。

## 姿勢: `OCHAKAI_MODE`

姿勢は二つの軸 — **呼び出し元を特定するか**と**誰かが書けるか** — の
組み合わせで、互いに排他なので一語で言う。

| `OCHAKAI_MODE` | 特定する | 書ける | 何のためか |
|---|---|---|---|
| 未設定 | する | できる | 通常のデプロイ。到達は Cloud Run IAM が決める |
| `read-only` | する | できない | 参照専用・凍結・監査のあいだ |
| `public` | しない | できない | 公開の読み取り専用デモ |
| `sandbox` | しない | できる | 書き戻しを試せる公開デモ。定期的に消える |
| `dev` | しない | できる | 手元の開発だけ。デプロイに使わない |

- **読めない綴りは起動エラー**である。既定に落とさない。
- **`read-only`** は書き込みをサービス層の一か所で断る(REST は 403、全
  応答に `Ochakai-Read-Only: true`)。MCP は書き込みのツールを**登録しない**、
  Web UI は書くボタンを出さない、CLI の `whoami` がそう言う。利用の記録は
  続ける(サーバー自身の観測なので)。
- **`public`** はヘッダを一つも読まず、全員が `human:anonymous` で、401 を
  返さない。公開したサービスではトークンは検証されていない自称だから
  である。書き込みを断り、ミスを記録しない。Terraform が `allUsers` を
  許すのは `public` と `sandbox` だけである。
- **`sandbox`** は匿名で書けて、運用者が定期的に復元する(ochakai は復元
  しない)。**そう言わないサンドボックスは書いたものを盗む**ので、
  `GET /api/v1/stats` が `sandbox: true` を返し、Web UI は全ページに
  バナーを出し、MCP の instructions がエージェントにそう伝える。ミスは
  記録しない。
- **`dev`** は認証を切った手元用で、委譲ヘッダは読む(統合を手元で試せる
  ように)。
- `public`・`sandbox`・`dev` は `OCHAKAI_OIDC_ISSUER` と組み合わせられず、
  `public`・`sandbox` はデータエージェントを入れられない。

## 環境変数

ochakai が読むのは次の 18 語で、それぞれの意味は
[configuration.md](../configuration.md) が持つ。

| 変数 | 領域 |
|---|---|
| `OCHAKAI_DATABASE_URL` / `OCHAKAI_DB_IAM_AUTH` / `PORT` | 接続 |
| `OCHAKAI_MODE` | 姿勢(上) |
| `OCHAKAI_OIDC_ISSUER` / `OCHAKAI_OIDC_AUDIENCE` / `OCHAKAI_DELEGATING_CALLERS` / `OCHAKAI_IAP_AUDIENCE` / `OCHAKAI_ADMINS` | [identity.md](identity.md) |
| `OCHAKAI_EMBEDDINGS` | [search.md](search.md) |
| `OCHAKAI_GCS_BUCKET` | [okf.md](okf.md)(無ければファイルは PostgreSQL) |
| `OCHAKAI_RECORD_MISSES` | [loop.md](loop.md) |
| `OCHAKAI_AGENT` / `OCHAKAI_OAUTH_CLIENT_ID` / `OCHAKAI_BIGQUERY_PROJECT` | [architecture.md](architecture.md) |
| `OCHAKAI_URL` / `OCHAKAI_PRODUCER` / `NO_COLOR` | CLI |

**値も名前も、読み違えたら止まる。**

- 値: 姿勢・埋め込み・エージェント・管理者の綴りなど、読めない値は起動
  エラーにする。`false` のつもりの綴りが既定に落ちて課金を続けるような
  壊れ方を許さない。
- 名前: **`OCHAKAI_` で始まり ochakai が読まない変数が一つでもあれば、
  `serve` と `serve-ui` は起動しない。** エラーは見つかった全部を名指し、
  読む変数を並べる。一文字違いで効いていない設定や、畳まれた後も残った
  古い変数に、運用者が気づく場所が他に無いからである。Cloud Run では
  起動に失敗したリビジョンにトラフィックが移らないので、代金は「新しい
  設定が出ない」で済む。
- 素通りするのは、テストの harness が使う `OCHAKAI_TEST_*` と、ochakai が
  配る Go 以外の読み手の名前(`config.KnownElsewhere`: 呼び出しフックの
  `OCHAKAI_RECALL_LIMIT`、BigQuery の job の `OCHAKAI_ID_TOKEN`)だけである。
  読む変数の一覧(`config.Known`)はソースの `os.Getenv` から読み戻して
  検査する。
- `PORT` と `NO_COLOR` は ochakai の名前空間の外の綴りを借りているだけで、
  断る対象ではない。
- CLI のコマンドは検査しない(`OCHAKAI_URL` の間違いはすぐ分かる)。

## やらないこと

- 姿勢をブールの組み合わせで言うこと、実行時の姿勢の切り替え。
- レート制限、監査ログ(認証の前段は Cloud Run IAM の仕事である)。
- 読めない値を既定に落とすこと、名前の間違いを警告で済ませること。
- 退役した名前に猶予を置くこと(名前が起動時に名指されること自体が
  猶予の代わりになる)。

## 経緯

設計記録 [0066](../design/0066-four-postures-one-word.md)・
[0087](../design/0087-a-sandbox-says-it-is-one.md)・
[0112](../design/0112-a-start-refuses-a-variable-it-does-not-read.md)、
数え方は [surface.md](../surface.md) の ENV。
