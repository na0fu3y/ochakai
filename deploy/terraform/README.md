# Cloud Run + Cloud SQL デプロイの Terraform モジュール

[deploy/cloudrun/README.md](../cloudrun/README.md) のベースラインをコードに
したもので、デプロイを diff としてレビューし、環境ごとに再現し、きれいに
壊せるようにする。

**このモジュールが ochakai を立ち上げる推奨の経路である** — `terraform
apply` 二回と、そのあいだの手作業ひとつ(スキーマの bootstrap)で済み、gcloud
ガイドを一手順ずつ辿るより短い。gcloud ガイドは引き続きリファレン
スであり正とする情報源である: 各部分がなぜそう作られているかを説明し、この
モジュールが省いているものすべてに触れている。それでも `terraform apply`
を動かすだけならガイドを読む必要は無い。二つが食い違えば、ガイドが正しく、
このモジュールの側にバグがある。

## 何を作るか

既定値はガイドのコスト最小化した例(月 $10 程度、そのほとんどが Cloud SQL
インスタンス)を一箇所だけ除いて再現する — **日次バックアップは既定で on**
である(ガイドの例は `--no-backup`)。データベースはナレッジベースそのもの
であり、そこだけは安さより先に立つ。上乗せはこの規模で月 $1 に届かない程度
で、使い捨ての評価なら `database_backups = false` で外せる:

| | |
|---|---|
| Artifact Registry | `ghcr.io` をプロキシする**リモートリポジトリ**(§1)— Cloud Run は GHCR から直接 pull できない |
| Cloud SQL | `db-f1-micro`、Postgres 17、10 GB SSD、単一ゾーン、日次バックアップ、`cloudsql.iam_authentication=on`(§2) |
| サービスアカウント | `ochakai-run`。`roles/cloudsql.client` + `roles/cloudsql.instanceUser` と、IAM データベースログイン(§3) |
| Cloud Run | ochakai サービス本体、scale-to-zero、**公開呼び出し不可**、`OCHAKAI_DB_IAM_AUTH=true`(§3) |
| IAM | 名指したメンバーへの `roles/run.invoker` — アクセス制御の面はこれがすべて |

Vertex AI embeddings(§4)はサーバーと合わせて**既定で on** である: モジュ
ールが `roles/aiplatform.user` を付与して API を有効化し、ochakai はそこか
ら自分のプロジェクトを見つける(設計ドキュメント 0147)。新しく作るベースは
`gemini-embedding-2` を `global` で使うので、本文と検索クエリはサービスの
リージョンの外へ出る(`embedding_model` で名指せば留められる)。既定で off なのは
private IP(§2b)、GCS files(§4b)、IAP 越しの web UI(§5b)。

### パスワードはどこにも無い

このモジュールに `random_password` は無く、Secret Manager のエントリも、
サービスアカウントキーも、どのリソースにも `password` 引数は無い — 代わり
にランタイムは Cloud SQL IAM データベース認証で Postgres に認証する。
**Secret-zero** はこのプロジェクトの中心的な設計判断であって細部ではない。
理由は
[デプロイガイド](../cloudrun/README.md#3-deploy-cloud-run-dedicated-identity-passwordless-org-restricted)
にある(設計ドキュメント 0065、0003)。

この先を読む前に知っておくべき帰結が二つある:

- モジュールはガイドのパスワード保持管理者ユーザーを**作らない**。人がデー
  タベースに直接アクセスする手段は代わりに IAM ログイン(`maintenance_users`)
  になる — どのみち運用ガイドの hardening チェックリストが行き着く先でも
  ある。
- したがって Terraform は一度きりのスキーマ bootstrap SQL を実行できない
  — Terraform からデータベースに接続するにはパスワードが要るからである。
  そのステップは手作業のまま残り、Cloud SQL の import として流す。下の
  「使い方」の 3 を見よ。

### 公開呼び出しは不可、名指しの例外が二つだけ

`invoker_members` は `allUsers` と `allAuthenticatedUsers` を拒む — ochakai
が provenance として信じるヘッダーは、Cloud Run の IAM チェックの背後にあ
ってはじめて信用できる
([要件と設定](../../docs/configuration.md#authentication-has-no-configuration))。
同じ規則が、Domain Restricted Sharing の org policy とデプロイの互換性を保
っている。

例外は `public_read_only`(ガイド §5d、設計ドキュメント 0066 §3)と
`sandbox`(同 §5d、設計ドキュメント 0087)の二つで、どちらもそれ自体が
`allUsers` を付与する。どちらも変数を二つに分けず一つにしているのは、公開
が安全なのはそれに伴うものと組み合わさったときだけだからである: 公開する
のと同じ変数が、identity を読むのをやめさせる。`public_read_only` はさら
にすべての書き込みを拒み、`sandbox` は書き込みを受ける代わりに、中身が消
えることを自分で宣言する。

だから、このモジュールの中に綴りが無いのは「公開かつ書き込み可能」ではな
く、**公開でありながら著者を記録し続ける** ochakai である — 誰とも確かめ
ようのない名前を記録に残す状態で、覚えておくべきチェックではなく、表現し
得ない状態である。

## 使い方

この節は上から順に実行する手順書である。コーディングエージェントに渡せば、
そのまま実行できるように書いてある。**「人に確かめる」と書いた点では止まり、
デプロイする人の返事を待つ。** そこで決めることは、課金と、誰が到達できるか
である。

初日に要る道具は `gcloud` と `terraform` の二つである。`cloud-sql-proxy` と
`psql` は、あとで人が直接データベースに入るときにだけ要る(下の
`maintenance_users`)。

### 0. 前提を確かめる

```sh
export PROJECT_ID=<your-project> REGION=<region>
gcloud auth list --filter=status:ACTIVE --format='value(account)'   # デプロイする本人か
gcloud auth application-default print-access-token >/dev/null \
  || gcloud auth application-default login                          # Terraform が使う資格情報
gcloud projects describe $PROJECT_ID --format='value(lifecycleState)'          # ACTIVE
gcloud billing projects describe $PROJECT_ID --format='value(billingEnabled)'  # True
gcloud org-policies describe constraints/sql.restrictPublicIp --project=$PROJECT_ID \
  --effective --format='value(spec.rules[0].enforce)'               # True なら下の「公開 IP を禁じた組織では」
gcloud run services list --project=$PROJECT_ID --format='value(metadata.name)'
gcloud sql instances list --project=$PROJECT_ID --format='value(name)'
```

最後の二つに `ochakai` が既にあれば、そのプロジェクトには先客がいる。
`name` を別の値にして、同じ名前のリソースを作らないようにする。
`gcloud auth application-default login` はブラウザでのサインインを求める
ので、エージェントが実行しているなら、人に頼む。

### 1. 変数を書く

```sh
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars
```

`project_id`・`region`・`image_tag` と `invoker_members` を書く。**人に
確かめる:** `invoker_members` に挙げた相手は、ナレッジのすべてを読み書き
できる。`image_tag` は最新のリリースの番号を書く(`latest` は書かない)。

### 2. 作る

```sh
terraform init
terraform plan -out=ochakai.plan
```

**人に確かめる:** plan の要約(`Plan: N to add, 0 to change, 0 to destroy`)と、
これで月 $10 程度の課金が始まること。新しいプロジェクトで `change` や
`destroy` が 0 でなければ、そこで止まって理由を調べる。

```sh
terraform apply ochakai.plan    # Cloud SQL の作成に 6〜15 分かかる
```

**この `apply` は、Cloud Run のサービスが起動できずにエラーで終わる。**
スキーマがまだ無い権限を求めるので、起動時のマイグレーションが
`permission denied for schema public` で止まるからである。それ以外の
リソースはできている。次の 3 で権限を付け、4 でサービスを作り直す。

### 3. スキーマを bootstrap する

SQL を一時バケットに置き、Cloud SQL 自身に `postgres` として流させる。
手元からデータベースに接続せず、パスワードを一度も設定しない。公開 IP の
有無によらず、同じ手順である。`<name>` は `var.name`(既定 `ochakai`)で、
インスタンスとデータベースの両方の名前である:

```sh
NAME=ochakai
BUCKET=gs://$PROJECT_ID-$NAME-bootstrap
terraform output -raw database_bootstrap_sql > bootstrap.sql
gcloud storage buckets create $BUCKET --project=$PROJECT_ID --location=$REGION --uniform-bucket-level-access
gcloud storage cp bootstrap.sql $BUCKET/
gcloud storage buckets add-iam-policy-binding $BUCKET \
  --member=serviceAccount:"$(gcloud sql instances describe $NAME --project=$PROJECT_ID --format='value(serviceAccountEmailAddress)')" \
  --role=roles/storage.objectViewer
gcloud sql import sql $NAME $BUCKET/bootstrap.sql --project=$PROJECT_ID --database=$NAME --user=postgres --quiet
gcloud storage rm -r $BUCKET
```

`maintenance_users` に名前を挙げた相手がいれば、`bootstrap.sql` を流す前に、
ランタイムが所有することになるテーブルを見せる一行を足しておく(起動時の
マイグレーションはサービスアカウントとしてテーブルを作るので、所有権は
デプロイした人ではなく書いた人に従う):

```sql
GRANT "<database_user output>" TO "you@your-org.example";
```

その相手はそれ以降、`cloud-sql-proxy --auto-iam-authn` と自分自身の identity
で接続する — こちらの経路にもパスワードは無い。

### 4. サービスを作り直し、確かめる

```sh
terraform apply                    # 起動できなかったサービスを作り直す
terraform output -raw use_command  # ochakai use <URL>
ochakai whoami                     # health: ok と、自分の名前
```

ここから先(ナレッジを入れる、エージェントを繋ぐ)は
[README](../../README.md#connect-an-agent) と
[最初のひと月](../../docs/guides/onboarding.md) にある。

### 公開 IP を禁じた組織では

0 の `sql.restrictPublicIp` が `True` なら、既定の構成の Cloud SQL は作れない。
`terraform.tfvars` に `enable_private_ip = true` を足す。3 の bootstrap は
そのまま使える。

**既存のデプロイが Service Networking のピアリングを持つネットワークには
当てない。** モジュールはピアリング用のアドレス
`google-managed-services-<network>` と接続を自分で作るので、同じネットワークの
既存のものとぶつかり、`terraform destroy` がそれを消す。次のコマンドが何かを
返すなら、そのネットワークには先客がいる:

```sh
gcloud services vpc-peerings list --network=default --project=$PROJECT_ID
```

**人に確かめる:** 先客がいるなら、専用の VPC とサブネットを作り、
`network` と `subnetwork` で名指してよいか。

```sh
gcloud compute networks create $NAME --project=$PROJECT_ID --subnet-mode=custom
gcloud compute networks subnets create $NAME --project=$PROJECT_ID \
  --network=$NAME --region=$REGION --range=10.90.0.0/24
```

2026-10-11 に、この組織ポリシーと先客のいるプロジェクトで、この節の手順を
通した(`docs/surface.md` の「経路 2 を歩いた記録」)。

### bootstrap が何をするか

拡張は `cloudsqlsuperuser` 権限を持つメンバーが作らなければならない —
Cloud SQL の pgvector は trusted extension ではない — そしてランタイムへ
のオブジェクト権限の付与も、誰かがやらなければならない。拡張を先に作って
おくことで、ランタイムの identity が持つのは明示的なオブジェクト権限だけ
になる: 起動時のマイグレーションは、権限を要しない
`CREATE EXTENSION IF NOT EXISTS` のスキップ経路にしか触れない。

IAM ログインが `cloudsqlsuperuser` になることは無いので、組み込みの
`postgres` ユーザーとして流す。`gcloud sql import sql` は Cloud SQL の管理
API の操作で、呼ぶ人の IAM(`cloudsql.instances.import`)で認可され、
`postgres` のパスワードを要らない。何も保存されず、管理者アクセスは
引き続き IAM でゲートされる。

## オプション

| 変数 | ガイド | 効果 |
|---|---|---|
| `enable_private_ip` | §2b | Cloud SQL の公開エンドポイントを落とす。peering range と Cloud Run 側の Direct VPC egress。無料。ローカルの `cloud-sql-proxy` アクセスには、その後 VPC に接続したワークステーションか一時的なパブリック IP が要る。 |
| `enable_vertex_embeddings` | §4 | **既定で on**: `roles/aiplatform.user` を付与し `aiplatform.googleapis.com` を有効化する。on にすると何が手に入るか、デプロイ後に何をすべきかは[デプロイガイド](../cloudrun/README.md#4-hybrid-semantic-search-vertex-ai-on-by-default)にある。`false` にすると `OCHAKAI_EMBEDDINGS=off` を渡し、何も付与しない。 |
| `embedding_model` | §4 | 既定は null — ochakai が自分のプロジェクトと製品既定のモデルを使う。モデル・リージョン・プロジェクトを選ぶなら Vertex AI のモデル resource name をそのまま渡す(`OCHAKAI_EMBEDDINGS` はこの一つで三つを運ぶ、設計ドキュメント 0147)。名指したデプロイは semantic search を**要求する**ので、pgvector の bootstrap を先に済ませておくこと。次元は設定できない — モデルのものであってデプロイのものではない。 |
| `agent_model` | §4c | `OCHAKAI_AGENT` — データエージェントを入れ、モデルを名指す。勧めるのは `global` の `gemini-3.8-flash`(resource name で名指す)。`roles/aiplatform.user` と API は `enable_vertex_embeddings` が false でも付く。 |
| `agent_oauth_client_id` | §4c | `OCHAKAI_OAUTH_CLIENT_ID` — エージェントが提案した SQL を、チームの Web UI が本人の権限で走らせる(`ochakai ui` はこれが無くても走らせる)。クライアントはコンソールで作る(Terraform では作れない)。 |
| `agent_bigquery_project` | §4c | `OCHAKAI_BIGQUERY_PROJECT` — 提案されたクエリの課金先。利用者は欄に何も書かずに済む。`enable_webui` なら `webui_iap_members` に `roles/bigquery.jobUser` を付与する。 |
| `enable_gcs_files` | §4b | バケット + `roles/storage.objectUser` + `OCHAKAI_GCS_BUCKET`。無ければファイルの実体は Cloud SQL に置かれる。 |
| `enable_webui` | §5b | `serve-ui` を、同じイメージ・専用の identity で、IAP の背後にある二つ目の Cloud Run サービスとして立てる。`webui_records_browser_user`(既定 on)は、UI のサービスアカウントではなくブラウザの本人を記録する。 |
| `read_only` | §5d | `OCHAKAI_MODE=read-only` を設定する — 非公開のままである。on にする*前*にベースへ import しておくこと: read-only なデプロイには import できない。 |
| `public_read_only` | §5d | `OCHAKAI_MODE=public` を設定し、`allUsers` を付与する(`sandbox` と並ぶ、このモジュールで `allUsers` が付く二つの変数の一つ)。`read_only` を含意する。その姿勢が何を手放すかは[デプロイガイド](../cloudrun/README.md#5d-optional-a-public-read-only-demo)にある。書き込み可能な状態で apply し、`ochakai import examples/demo` した後、これを on にして再度 apply する。 |
| `sandbox` | §5d | `OCHAKAI_MODE=sandbox` を設定し、`allUsers` を付与する — **匿名で書ける**デプロイなので、中身が消えることを製品自身が宣言する(設計ドキュメント 0087)。`public_read_only` とは併用しない。復元のジョブは運用ガイドの[使い捨てのサンドボックス](../../docs/guides/operating.md#sandbox)にある。 |
| `maintenance_users` | §6 | 人のための Cloud SQL IAM ログイン、パスワードは無い。 |
| `database_backups` | §6 | 日次バックアップ。**既定で on** — データベースはナレッジベースそのものである。使い捨ての評価だけ off にしてよい。 |

web UI を有効にすると `google-beta` provider が付いてくる: 残っている
ベータ面は `google_project_service_identity`(IAP のサービスエージェント)
だけである。Cloud Run 上の IAP 自体は 2026-03 に GA になり、`iap_enabled`
も GA provider 7.21 に入ったので、webui サービス本体はもう beta を通らない。
provider はどちらにせよ宣言されているので、UI が off のときも download は
される — その場合は何もしない。

## このモジュールが扱わないもの

意図してのことである。それぞれ文書化されている — ほとんどはガイドに、
hardening とアップグレードは
[運用ガイド](../../docs/guides/operating.md)にある。

- **スキーマ bootstrap SQL**(ガイド §3)— Terraform からは接続できない。
  上の「使い方」の 3 で、Cloud SQL の import として流す。
- **ナレッジの読み込みと Claude Code の接続**(ガイド §5)— クライアント
  側の話で、CLI に必要な設定は `ochakai use` 以外に無い。
- **自分のアプリケーションのための delegated provenance**(ガイド §5c)
  — 変数はここにある(`delegating_callers`)が、アプリケーションとその
  サービスアカウントをデプロイするのはあなた自身である。
- **Org-policy によるガードレール**(運用ガイドの
  [Hardening](../../docs/guides/operating.md#hardening):
  `sql.restrictAuthorizedNetworks`、`sql.restrictPublicIp`、
  `iam.allowedPolicyMemberDomains`)— これらは個々のデプロイより上、組織
  のレベルに属し、Org Policy Administrator ロールが要る。
- **パスワード管理者ユーザーの退役**(同じ節)— 退役させるものが無い:
  このモジュールはそもそも作らない。
- **`--ssl-mode=ENCRYPTED_ONLY`、point-in-time recovery、Binary
  Authorization、Cloud SQL のデータアクセス監査ログ**(同じ節)—
  baseline を超える hardening で、それぞれ `gcloud` コマンド一つか
  console の設定一つで済む。
- **既存デプロイのアップグレード**([Upgrades](../../docs/guides/operating.md#upgrades))
  — `image_tag` を変えて apply する(webui がサーバーより先に届かなけれ
  ばならない、あの一つのアップグレードでは `webui_image_tag` を先に)。
  マイグレーションは起動時に走る。そこにあるバージョンごとの注意点はその
  まま当てはまり、gcloud で構築済みの既存デプロイをこのモジュールの
  state に import することを、このモジュールは楽にしようとはしていない。

## 取り壊し

`deletion_protection` は既定で true であり、Cloud SQL インスタンスと
Cloud Run サービスの両方を、Terraform と API の両方のレベルで守っている。
だから最初にこれを外す必要がある:

```sh
terraform apply -var deletion_protection=false
terraform destroy
```

files が有効なら、バケットはオブジェクトを持っている間 destroy を
拒む — それは誰かが追加したすべてのファイルの、唯一のコピーだからである。
本気でやるときは、同じ `apply` に `-var gcs_force_destroy=true` を足す。

有効にした API は有効なままにする: 一つを off にするのはプロジェクト全体
に及ぶ行為であり、他の何かがそれに依存しているかもしれない。
