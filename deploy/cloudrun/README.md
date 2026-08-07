# ochakai を Cloud Run + Cloud SQL にデプロイする

推奨構成は、**組織限定でトークンを持たない** ochakai である。誰が届くか
は Cloud Run の IAM が決め、ochakai は誰が何をしたか(provenance)を記録
するだけで、自前の認可は行わない。Cloud Run は scale-to-zero し、最小の
Cloud SQL インスタンス一つでナレッジベース全体を賄う — コンテナイメージ
一つとデータベース一つで済む。

**コスト(概算、us-central1):**

| コンポーネント | 構成 | 月額 |
|---|---|---|
| Cloud SQL | `db-f1-micro`(shared core)、10 GB SSD、単一ゾーン、バックアップ無し | ~$9–10 |
| Cloud Run | リクエスト従量課金、`min-instances=0` | アイドル時 ~$0 |
| Vertex AI embeddings(既定で on、§4) | `gemini-embedding-001`、トークン従量課金 | このガイドの規模では数セント |

料金のほとんどは Cloud SQL が占める。アジアのリージョン(例:
`asia-northeast1`)はやや高くなる — レイテンシの要件に合わせて選べば
よい。取り壊しのコマンドは末尾にある。

**推奨: [deploy/terraform](../terraform) で立ち上げる** — §1–§4b(web UI
とデモの姿勢を含む)を 13 手順の `terraform apply` にまとめてあり、diff
としてレビューでき、環境ごとに再現でき、きれいに壊せる。このガイドは引
き続きリファレンスであり続ける — 各リソースがなぜそう作られているかを
説明し、コマンドを手で打ちたい場合の経路でもある。§1–§5、§5d、§9 をカ
バーし、web UI・公開デモ・org-policy によるガードレール・アップグレー
ドの注意点は運用ガイドが、
[docs/guides/rest-integration.md](../../docs/guides/rest-integration.md)
は §5c と REST API を組み込むアプリケーションに要るその他すべてをカバー
する。

**すでにデプロイ済みなら?**
[docs/guides/operating.md](../../docs/guides/operating.md) がその後を扱
う — バックアップと復元、hardening、team web UI、監視、capacity、アッ
プグレード。

**自分の製品に ochakai を組み込むなら?**
[docs/guides/rest-integration.md](../../docs/guides/rest-integration.md)
が、REST API を直接叩くアプリケーションのための認証、delegated
provenance、安全な同時書き込みをカバーする。

## 1. 前提条件

このガイドが使うローカルツールは、`gcloud` 自身(認証済みであること —
`gcloud auth login`)のほかに: [Go toolchain](https://go.dev/dl/)
(`go run` / `go install`、§5)、
[`cloud-sql-proxy`](https://cloud.google.com/sql/docs/postgres/sql-proxy)
と `psql`(§3 の一度きりのデータベース設定)、`openssl`(§2 の admin パ
スワード)、それに任意で [`gh` CLI](https://cli.github.com)(すぐ下の
バージョン検索用 — 無ければ
[リリースページ](https://github.com/na0fu3y/ochakai/releases)から手で
読む)。

```sh
export PROJECT_ID=<your-project>
export REGION=us-central1
gcloud config set project $PROJECT_ID
gcloud services enable run.googleapis.com sqladmin.googleapis.com \
  sql-component.googleapis.com artifactregistry.googleapis.com
```

Cloud Run は ghcr.io から直接イメージを pull できないので、GHCR をプロ
キシする Artifact Registry の**リモートリポジトリ**を作る。レイヤーは最
初の pull でキャッシュされ、イメージの digest は GHCR のものと同一のま
まなので、GHCR の attestation(`gh attestation verify`)は動かすものに
対してそのまま有効である:

```sh
gcloud artifacts repositories create ghcr \
  --repository-format=docker \
  --mode=remote-repository \
  --remote-docker-repo=https://ghcr.io \
  --location=$REGION

export VERSION=$(gh release view --repo na0fu3y/ochakai --json tagName -q .tagName | tr -d v)
export IMAGE=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:$VERSION
```

`:latest` ではなくバージョンを固定し、再デプロイを意識的な判断にする。
`gh` CLI が無ければ
[リリースページ](https://github.com/na0fu3y/ochakai/releases)から番号
を読み、手で設定する — `export VERSION=0.20.0`。

(このガイドは 0.9.0 以降を前提にしている。それより前のリリースは
[retract](https://go.dev/ref/mod#go-mod-file-retract) 済みでサポート外
であり、動かしている場合はアップグレード経路を §8 で見よ。)

## 2. データベースを作る(最安構成)

```sh
gcloud sql instances create ochakai \
  --database-version=POSTGRES_17 \
  --edition=enterprise \
  --tier=db-f1-micro \
  --region=$REGION \
  --storage-size=10 \
  --storage-type=SSD \
  --no-storage-auto-increase \
  --no-backup \
  --database-flags=cloudsql.iam_authentication=on

gcloud sql databases create ochakai --instance=ochakai

# admin user for local import/maintenance only — its password never
# reaches Cloud Run (the service itself connects passwordless, §3)
export DB_PASSWORD=$(openssl rand -hex 24)
gcloud sql users create ochakai --instance=ochakai --password=$DB_PASSWORD
```

補足:

- インスタンス作成には 10–15 分かかる。
- `--no-backup` はこの例を安く保つためのもの。大事なものにはバックアッ
  プを有効にする
  (`gcloud sql instances patch ochakai --backup-start-time=03:00` —
  `patch` に素の `--backup` フラグは無い。有効化とは開始時刻を選ぶこと
  である)。
- Cloud SQL の API 経由で作ったユーザー(この admin ユーザーもそう)は
  `cloudsqlsuperuser` のメンバーになり、拡張を作成できる。ランタイムの
  サービスアカウントは意図してそのどちらも持たない(§3)。
- **公開 IP について**: これはインターネットに開かれてはいない。
  authorized-networks のリストが空(既定)なら直接接続は落とされ、入る
  経路は Cloud SQL コネクタ — IAM チェック(`cloudsql.instances.connect`)
  と mTLS を経て、そのあとデータベース認証 — だけになる。ローカルの
  admin アクセス(§3 の SQL、§6 の保守)は
  [`cloud-sql-proxy`](https://cloud.google.com/sql/docs/postgres/sql-proxy)
  を通す。これも同じコネクタ経路を使うので、リストに concept を持たせ
  る必要は無い。空のままにしておけば、この姿勢は保たれる。到達可能な
  エンドポイントそのものを無くすには §2b を見よ。

### 2b. 任意の hardening: private IP のみにする

本番に近いデプロイでは、公開 IP を完全に落とす — 無料であり、ちょっと
した試用を超えるならやる価値がある。コマンドと、ローカルの admin アク
セスとのトレードオフは運用ガイドの
[Hardening](../../docs/guides/operating.md#hardening) にある。

### 2c. 代わりに: 既存のインスタンスに同居させる

企業環境の典型は、インスタンスを新設せず既存の PostgreSQL インスタンス
に database を一つ足す形である。その場合 §2 は丸ごと飛ばし、§3 以降を
インスタンスの接続名を読み替えてそのまま辿る。先に要るのは二つだけ:

```sh
gcloud sql instances patch <instance> --database-flags=cloudsql.iam_authentication=on
gcloud sql databases create ochakai --instance=<instance>
```

- `--database-flags` は一覧の**全置換**である — インスタンスに既に
  フラグがあるなら、同じコマンドに列挙し直す。そしてフラグの変更は
  **インスタンスの再起動を伴う**ので、同居しているデータベースの
  メンテナンス窓で行う(既に on なら patch ごと不要である)。
- §2 の admin ユーザーは作らなくてよい — §3 の bootstrap SQL は、
  既存の管理者ユーザーで打てば足りる。
- インスタンスが **private IP のみ**なら、§3 の `gcloud run deploy` に
  `--network=<vpc> --subnet=<subnet>` を足して Cloud Run を同じ VPC に
  入れる(Direct VPC egress)— 無いと Cloud SQL コネクタはタイムアウト
  で死ぬ(§7)。フラグの意味は運用ガイドの
  [Hardening](../../docs/guides/operating.md#hardening) にある。

<a id="3-deploy-cloud-run-dedicated-identity-passwordless-org-restricted"></a>

## 3. Cloud Run をデプロイする(専用の identity、パスワードレス、組織限定)

ochakai 専用のサービスアカウントを作り、**IAM データベース認証**でデー
タベースにログインさせる — 接続パスワードは、接続時に取得する短命の
IAM トークンになるので、**デプロイのどこにもデータベースパスワードが
存在しない**:

```sh
gcloud iam service-accounts create ochakai-run --display-name="ochakai service"
export SERVICE_ACCOUNT=ochakai-run@$PROJECT_ID.iam.gserviceaccount.com

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/cloudsql.client
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/cloudsql.instanceUser

# database principal for the service account (note: no .gserviceaccount.com)
export DB_SA_USER=ochakai-run@$PROJECT_ID.iam
gcloud sql users create $DB_SA_USER --instance=ochakai --type=cloud_iam_service_account
```

サービスアカウントのためにデータベースを準備する(一度きり、admin ユー
ザーとして —
`cloud-sql-proxy $PROJECT_ID:$REGION:ochakai --port 55432` と
`psql "host=localhost port=55432 dbname=ochakai user=ochakai"` で接続
し、§2 の `$DB_PASSWORD` を使う)。拡張はここで先に作っておくので、ラ
ンタイムが昇格した権限を必要とすることは無い: ochakai の起動時マイグ
レーションが触れるのは、権限を要しない
`CREATE EXTENSION IF NOT EXISTS` のスキップ経路だけである。それ以外は
すべて明示的なオブジェクト権限であり — ランタイムの identity に
**`cloudsqlsuperuser` も role membership も無い**:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS vector;   -- for §4's semantic search, which is on
                                         -- by default; Cloud SQL's pgvector is not
                                         -- a trusted extension, hence admin-created
GRANT USAGE, CREATE ON SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL TABLES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "ochakai-run@<PROJECT_ID>.iam";
```

専用の identity と `OCHAKAI_DB_IAM_AUTH`(パスワードレスなデータベー
ス)で非公開にデプロイし、そのあと組織に invoke を許可する:

```sh
gcloud run deploy ochakai \
  --image=$IMAGE \
  --region=$REGION \
  --service-account=$SERVICE_ACCOUNT \
  --no-allow-unauthenticated \
  --min-instances=0 --max-instances=1 \
  --cpu=1 --memory=512Mi \
  --add-cloudsql-instances=$PROJECT_ID:$REGION:ochakai \
  --set-env-vars="OCHAKAI_DB_IAM_AUTH=true" \
  --set-env-vars="OCHAKAI_DATABASE_URL=postgres:///ochakai?host=/cloudsql/$PROJECT_ID:$REGION:ochakai&user=$DB_SA_USER"

gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=domain:your-org.example --role=roles/run.invoker

export OCHAKAI_URL=$(gcloud run services describe ochakai --region=$REGION --format='value(status.url)')
```

**もう一つ、一度きりの手順が残っている。最初のデプロイが起動時マイグ
レーションを終えたところで**: マイグレーションが作るテーブルの所有者
は**ランタイムのサービスアカウント**である(import は API 経由なので、
admin として何かが作られることは無い)。admin ユーザーがそれらのテー
ブルを直接扱える(保守、ad-hoc な SQL)ようにするには、ランタイムの
role を与える — 上と同じ要領で、§2 の `$DB_PASSWORD` を使って再接続す
る:

```sql
GRANT "ochakai-run@<PROJECT_ID>.iam" TO "ochakai";
```

仕組み:

- **誰がサービスに届くかは Cloud Run IAM が決める**(組織のメンバー
  と、`roles/run.invoker` を付与したサービスアカウント。匿名リクエス
  トはコンテナに届く前に Google の 401 を受け取る)。ochakai はその
  identity を provenance として記録する — アクセスモデル全体は
  [要件と設定](../../docs/configuration.md#authentication-has-no-configuration)
  を見よ。
- **サービスを公開 invoke 可能(`allUsers`)にしては絶対にいけない** —
  ochakai が読む identity ヘッダーは、Cloud Run の IAM チェックの背後
  にあってはじめて信用できる。唯一の例外は、identity を一切読まず何
  も書き込まないデプロイ(§5d のデモ)であり、書き込み可能なものには
  この規則に例外は無い。
- このデプロイに `allUsers` 付与は要らないので、Domain Restricted
  Sharing の org policy(`iam.allowedPolicyMemberDomains`)と両立する
  — そしてそれを維持しておく良い理由でもある。
- `OCHAKAI_DB_IAM_AUTH` を使うと `OCHAKAI_DATABASE_URL` にパスワード
  が含まれなくなるので、環境変数に秘密は何も無い。(代わりにパスワー
  ド認証を使うなら、URL は `--set-secrets` で Secret Manager に置く。)
- デプロイ全体が **secret-zero** である: クライアントは Google の
  identity を持ち込み、ochakai は自分のサービスアカウントの identity
  をデータベースに持ち込む — 発行も保存もローテーションも何一つ要ら
  ない。

[Cloud Run proxy](https://cloud.google.com/sdk/gcloud/reference/run/services/proxy)
経由で確認する(直接の `curl` は IAM に阻まれる — それが狙いである):

```sh
gcloud run services proxy ochakai --region=$REGION --port=8787 &
curl http://localhost:8787/health
```

注: `/healthz` ではなく `/health` を使う — Google Frontend は
`run.app` の URL 上で `/healthz` を横取りし、アプリに届く前に自前の
404 を返す。

<a id="4-hybrid-semantic-search-vertex-ai-on-by-default"></a>

## 4. ハイブリッドなセマンティック検索(Vertex AI、既定で on)

Cloud Run 上では、ochakai はメタデータサーバーに自分がどのプロジェク
トで動いているかを尋ね、それに合わせてセマンティック検索を有効にする
(設計ドキュメント 0080)— 設定する変数は無く、認証は ADC 経由のサー
ビス identity なので、ここでも API キーは無い。

**実際に使えるかどうかを決めるのは設定ではなく IAM である。**
`roles/aiplatform.user` が無ければサービス identity は Vertex AI を呼
べない: ochakai は起動時に小さな埋め込みを一つ試してそれを知り、ログ
一行でそう告げて、字句検索だけを提供する。だからこの節はコマンド二つ
で済み、それがハイブリッドと字句検索だけの違いになる:

```sh
gcloud services enable aiplatform.googleapis.com

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/aiplatform.user   # ochakai-run SA from §3
```

理由が無ければ付与しておく — とりわけ日本語で書かれたナレッジベース
では、trigram 索引は 2 文字の語を引けず、字句検索の側はスキャンで答え
ることになる。次の起動で ochakai は pgvector のテーブルを作り、検索は
ハイブリッド(trigram + vector)になる。その後 Vertex AI が使えなくな
っても、trigram だけへ穏やかにフォールバックする。

**拒むには**、role を付与しないままにするか(何も呼ばれず、課金も発生
しない)、そう明示する:

```sh
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_EMBEDDINGS=off
```

`--set-env-vars` ではなく `--update-env-vars` を使う: 前者はすべての
環境変数を**置き換える**ので `OCHAKAI_DATABASE_URL` を消してしまう。

role を付与する前に知っておく価値があること: ochakai には認可が無いの
で(設計ドキュメント 0065 §1)、**サービスに届く誰もが、書き込むことで
Vertex AI の呼び出しを引き起こせる** — キュレーションされたナレッジ
ベースの規模では数セントで済む。

別のモデル・リージョン・プロジェクトを使うなら、変数は同じ一つで、値が
Vertex AI のモデル resource name になる(設計ドキュメント 0080):

```sh
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_EMBEDDINGS=projects/$PROJECT_ID/locations/global/publishers/google/models/gemini-embedding-2
```

これは**セマンティック検索を要求する**綴りである — 使えなければ起動を
拒否する。マルチモーダルなファイル検索と、role 付与前や model 変更前に
読み込んだベースを埋め直すコマンド `ochakai reembed` は
[環境変数](../../docs/configuration.md#environment-variables)にある。
model を変えることが既存のデータベースに何をするかは、運用ガイドの
[Upgrades](../../docs/guides/operating.md#upgrades) にある。

## 4b. ファイルには GCS が要る

ファイルの実体は GCS バケットにのみ置かれる — メタデータとリビジョン
は Postgres に残り、認証はサービス identity 経由の ADC でキーは無い。
`OCHAKAI_GCS_BUCKET` が無いとサービスは markdown 専用で動く: ファイル
の書き込みは 501 を返し、import はファイルを失敗として報告する。ファイ
ルを一切置かないのでなければ、この節は飛ばさない:

```sh
gcloud storage buckets create gs://$PROJECT_ID-ochakai-blobs \
  --location=$REGION --uniform-bucket-level-access --public-access-prevention

gcloud storage buckets add-iam-policy-binding gs://$PROJECT_ID-ochakai-blobs \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/storage.objectUser  # ochakai-run SA from §3

gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_GCS_BUCKET=$PROJECT_ID-ochakai-blobs
```

ファイルの実体はそのままバケットへ行く(オブジェクトは
`blob/<sha256>` として content-addressed され、作成のみで削除されるこ
とは無い)。

**添付があってバケットが無い 0.8.x 以下からのアップグレード**:
`OCHAKAI_GCS_BUCKET` を設定した 0.8.x のリリースを一度動かす — その起
動時 backfill が、Postgres 内の添付バイトをバケットへ移す。0.9.0 以降
は backfill が無くなっており、添付バイトがまだ inline のままだと
migration 0009 が実行を拒む — サービスは指示付きで起動に失敗するだけ
で、何も失われない。一度移行すれば bytea 列は無くなるので、バケット無
しのバイナリや設定はもう添付を読めなくなる — その後は変数を設定した
ままにしておく。

## 5. ナレッジを読み込み、Claude Code を接続する

API 経由で concept を一つ登録する。CLI は自分の gcloud ログインから
Google ID トークンを解決するので、Cloud SQL proxy も authorized
network も要らない — `$OCHAKAI_URL` は上でサービスをデプロイしたとき
に export 済みである:

```sh
curl -fsSL "https://raw.githubusercontent.com/na0fu3y/ochakai/v$VERSION/examples/golden-query.md" | \
  go run github.com/na0fu3y/ochakai/cmd/ochakai@latest put queries/monthly-revenue
```

自分のテーブルを入れるなら
[examples/bigquery-catalog](../../examples/bigquery-catalog) に day-one の
手順と、それを走らせる job と、走りが正しかったかを判定する attester が
ある。**§4b の GCS バケットが要る** — バンドルが計算本体と attester を
ファイルとして運ぶので、バケットの無いインスタンスは concept だけ入れて
最初の `.py` で落ちる。

Claude Code — あるいはシェルを持つ任意のエージェント — を同じ URL に
向ける。上で走らせた CLI が、proxy 無しでトークンを解決できることをす
でに証明している。これが README の
[Connect an agent](../../README.md#connect-an-agent)に沿った、推奨の
入り方である:

```sh
go install github.com/na0fu3y/ochakai/cmd/ochakai@latest
ochakai use $OCHAKAI_URL
```

代わりに Claude Code の中で MCP ツールを使いたいなら、ブリッジにも
proxy は要らない — `claude mcp add ochakai -- ochakai mcp-stdio`
(`gcloud auth login` と `PATH` 上の `ochakai` が要る。
[MCP クライアントを繋ぐ](../../docs/guides/mcp-clients.md#what-the-bridge-needs)
を見よ)。

§3 と同じ
[Cloud Run proxy](https://cloud.google.com/sdk/gcloud/reference/run/services/proxy)
経由で REST の smoke test を行う(直接の `curl` は IAM に阻まれる):

```sh
gcloud run services proxy ochakai --region=$REGION --port=8787 &
curl "http://localhost:8787/api/v1/search?q=revenue"
```

## 5b. 任意: IAP の背後の team web UI(意図して別サービス)

web UI は `serve` の中ではなく、**それ自身のサービス**として
[Identity-Aware Proxy](https://cloud.google.com/iap/docs/enabling-cloud-run)
の背後で動く — Go の CLI を動かせない人がブラウザからのアクセスを必要
とするときにデプロイする。サービスアカウントではなくブラウザの本人を
記録することを含め、コマンドは運用ガイドの
[The team web UI](../../docs/guides/operating.md#the-team-web-ui) にあ
る。

## 5c. 任意: 多くの人に使わせるアプリケーション(delegated provenance)

自分の製品に ochakai を組み込む — データ分析のチャットアプリ、web フ
ロントエンドを持つ社内エージェント — と、§5 の経路には無かった問題に
ぶつかる: アプリケーションは*自分の*サービスアカウントで Cloud Run に
届くので、その利用者が書くすべての concept が、その一つの identity と
して記録されてしまう。ochakai が売り物にしているものの大半である
provenance が崩れる。

その直し方、二コマンドの設定、そして REST API を組み込むアプリケーシ
ョンに要るその他すべて — 認証、delegated provenance、
`Ochakai-Producer`、安全な同時書き込み — は
[REST API を自分のサービスに組み込む](../../docs/guides/rest-integration.md)
にある。

<a id="5d-optional-a-public-read-only-demo-the-one-public-posture"></a>

## 5d. 任意: 公開の read-only なデモ(唯一の public な姿勢)

ここまでは一貫して `allUsers` を禁じており、それは書き込み可能なあら
ゆるデプロイについて変わらない。デモだけが唯一の例外であり、それも手
放すものがあるからこそ許される: `OCHAKAI_MODE=public` は identity を
一切読まず、書き込みも伴わない(設計ドキュメント 0066 §3)— 誰も
記録せず何も書き込まないサービスだけが、誰にでも開いて安全な唯一の種
類である。

種を入れること、公開サービスが見知らぬ相手の `Authorization` ヘッダー
をまだ信じてしまう窓を作らずに mode を切り替えること、切り替えが反映
されたことの確認、コスト、手放すもの、そして元に戻すことは、すべて運
用ガイドの [Public demo](../../docs/guides/operating.md#public-demo)
にある。

## 6. セキュリティ hardening のチェックリスト

既定の §1–§5 のデプロイは、すでに secret-zero かつ least-privilege で
ある。さらにハードルを上げる org-policy によるガードレール、TLS の強
制、最後のパスワードの退役、バックアップ、デプロイ時のイメージ
gating は、すべて運用ガイドの
[Hardening](../../docs/guides/operating.md#hardening) にある。

## 7. セキュリティを固めた組織でのトラブルシューティング

- **`allUsers` の binding が "do not belong to a permitted customer" で
  失敗する**: 組織が Domain Restricted Sharing
  (`iam.allowedPolicyMemberDomains`)を強制している。通常のデプロイ
  に `allUsers` は要らない(webui は IAP の背後、§5b)ので、ポリシー
  は on のままにする — §5d のデモを立てていてこれに当たったら、組織
  ではなくそのプロジェクトを例外にする。
- **サービスが Ready なのに `run.app` が Google の HTML 404
  ("That's an error")を返す**: インフラを疑う前に、実際のアプリケー
  ションのエンドポイント(例: `/api/v1/search?q=x`)を叩き、リクエス
  トログを確認する。二つの Google Frontend の挙動が重なって、健全な
  サービスが死んでいるように見える:
  1. `/healthz` は `run.app` 上で Google Frontend に横取りされ、コン
     テナに届くことなく(リクエストログも残さず)404 になる。
     `/health` を使う。
  2. アプリケーションレベルの 404 応答は Google のブランド付き 404
     ページに装われるので、未対応のパスがルーティングの失敗に見え
     る。
  本当に存在しないサービス URL は、もっと短い 404 ページを返す
  (~272 バイト対 ~1.5 KB)— `content-length` を比べれば見分けが付
  く。
- **コンテナが `cloudsql.instances.get ... NOT_AUTHORIZED` で終了す
  る**: サービスアカウントに `roles/cloudsql.client` が無い(§3 の最
  初の手順)。
- **コンテナが `connection to Cloud SQL instance at 10.x.x.x:3307
  failed: … timed out` で終了する**: インスタンスは private IP のみ
  なのに、Cloud Run サービスが VPC の中に居ない。
  `gcloud run services update ochakai --region=$REGION --network=<vpc> --subnet=<subnet>`
  で Direct VPC egress を足す(§2c、運用ガイドの
  [Hardening](../../docs/guides/operating.md#hardening))。
- **サービスアカウントを作った直後に Cloud SQL ソケットが
  `connection refused` になる**: 作りたてのサービスアカウントへの
  IAM 付与が反映されるまで 1 分ほどかかることがある。role が届いて
  いるか確認し
  (`gcloud projects get-iam-policy ... --filter=bindings.members:ochakai-run@`)、
  再デプロイする。

## 8. 既存デプロイのアップグレード

サービスを新しいタグに向ける。マイグレーションは起動時に自動で走る。
バージョンごとの breaking change の注意点を含む完全なアップグレード経
路は、運用ガイドの
[Upgrades](../../docs/guides/operating.md#upgrades) にある。

## 9. 取り壊し

```sh
gcloud run services delete ochakai --region=$REGION --quiet
gcloud run services delete ochakai-webui --region=$REGION --quiet
gcloud sql instances delete ochakai --quiet
gcloud storage rm -r gs://$PROJECT_ID-ochakai-blobs --quiet  # if §4b was used
# if §2b was used:
gcloud services vpc-peerings delete --service=servicenetworking.googleapis.com --network=default --quiet
gcloud compute addresses delete google-managed-services-default --global --quiet
```
