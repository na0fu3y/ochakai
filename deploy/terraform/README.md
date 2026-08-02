# Cloud Run + Cloud SQL デプロイの Terraform モジュール

[deploy/cloudrun/README.md](../cloudrun/README.md) のベースラインをコードに
したもので、デプロイを diff としてレビューし、環境ごとに再現し、きれいに
壊せるようにする。

**このモジュールが ochakai を立ち上げる推奨の経路である** — 下の 13 手順で
済み、gcloud ガイドの 36 手順より少ない。gcloud ガイドは引き続きリファレン
スであり正とする情報源である: 各部分がなぜそう作られているかを説明し、この
モジュールが省いているものすべてに触れている。それでも `terraform apply`
を動かすだけならガイドを読む必要は無い。二つが食い違えば、ガイドが正しく、
このモジュールの側にバグがある。

## 何を作るか

既定値はガイドのコスト最小化した例(月 $10 程度、そのほとんどが Cloud SQL
インスタンス)を再現する:

| | |
|---|---|
| Artifact Registry | `ghcr.io` をプロキシする**リモートリポジトリ**(§1)— Cloud Run は GHCR から直接 pull できない |
| Cloud SQL | `db-f1-micro`、Postgres 17、10 GB SSD、単一ゾーン、バックアップ無し、`cloudsql.iam_authentication=on`(§2) |
| サービスアカウント | `ochakai-run`。`roles/cloudsql.client` + `roles/cloudsql.instanceUser` と、IAM データベースログイン(§3) |
| Cloud Run | ochakai サービス本体、scale-to-zero、**公開呼び出し不可**、`OCHAKAI_DB_IAM_AUTH=true`(§3) |
| IAM | 名指したメンバーへの `roles/run.invoker` — アクセス制御の面はこれがすべて |

Vertex AI embeddings(§4)はサーバーと合わせて**既定で on** である: モジュ
ールが `roles/aiplatform.user` を付与して API を有効化し、ochakai はそこか
ら自分のプロジェクトを見つける(設計ドキュメント 0073)。既定で off なのは
private IP(§2b)、GCS attachments(§4b)、IAP 越しの web UI(§5b)。

### パスワードはどこにも無い

このモジュールに `random_password` は無く、Secret Manager の concept も、
サービスアカウントキーも、どのリソースにも `password` 引数は無い — 代わり
にランタイムは Cloud SQL IAM データベース認証で Postgres に認証する。
**Secret-zero** はこのプロジェクトの中心的な設計判断であって細部ではない。
理由は
[デプロイガイド](../cloudrun/README.md#3-deploy-cloud-run-dedicated-identity-passwordless-org-restricted)
にある(設計ドキュメント 0002、0003)。

この先を読む前に知っておくべき帰結が二つある:

- モジュールはガイドのパスワード保持管理者ユーザーを**作らない**。人がデー
  タベースに直接アクセスする手段は代わりに IAM ログイン(`maintenance_users`)
  になる — どのみち運用ガイドの hardening チェックリストが行き着く先でも
  ある。
- したがって Terraform は一度きりのスキーマ bootstrap SQL を実行できない
  — 実行するにはパスワードを持つ必要があるからである。そのステップは手作
  業のまま残る。下を見よ。

### 公開呼び出しは不可、名指しの例外が一つだけ

`invoker_members` は `allUsers` と `allAuthenticatedUsers` を拒む — ochakai
が provenance として信じるヘッダーは、Cloud Run の IAM チェックの背後にあ
ってはじめて信用できる
([要件と設定](../../docs/configuration.md#authentication-has-no-configuration))。
同じ規則が、Domain Restricted Sharing の org policy とデプロイの互換性を保
っている。

例外は `public_read_only`(ガイド §5d、設計ドキュメント 0042)で、これ自体
が `allUsers` を付与する。変数を二つに分けず一つにしているのは、public が
安全なのはそれに伴うものと組み合わさったときだけだからである: デプロイはす
べての書き込みを拒み、identity を一切読まない — 偽装する provenance も、取
り違える著者も存在しない。だから public で*書き込み可能*な ochakai には、
このモジュールの中に綴りが無い — 覚えておくべきチェックではなく、表現し得
ない状態である。

## 使い方

```sh
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars   # edit: project, region, image tag, invokers
terraform init
terraform plan
terraform apply    # Cloud SQL instance creation takes 10-15 minutes
```

そのあと手作業を一つ済ませれば、サービスの準備が整う:

```sh
terraform output -raw database_bootstrap_sql
terraform output -raw use_command
```

### 唯一の手作業: スキーマの bootstrap

拡張は `cloudsqlsuperuser` 権限を持つメンバーが作らなければならない —
Cloud SQL の pgvector は trusted extension ではない — そしてランタイムへ
のオブジェクト権限の付与も、誰かがやらなければならない。拡張を先に作って
おくことで、ランタイムの identity が持つのは明示的なオブジェクト権限だけ
になる: 起動時のマイグレーションは、権限を要しない
`CREATE EXTENSION IF NOT EXISTS` のスキップ経路にしか触れない。

IAM ログインが `cloudsqlsuperuser` になることは無いので、組み込みの
`postgres` ユーザーを使い、保存せずその場で作って捨てる break-glass パス
ワードを使う(運用ガイドの hardening チェックリスト):

```sh
gcloud sql users set-password postgres --instance=ochakai --prompt-for-password
cloud-sql-proxy "$(terraform output -raw sql_connection_name)" --port 55432 &
psql "host=localhost port=55432 dbname=ochakai user=postgres"
# paste: terraform output -raw database_bootstrap_sql
```

そのあとパスワードを、また別の捨てる値に変えておく。何も保存されず、管理
者アクセスは引き続き IAM でゲートされる。

`maintenance_users` に名前を挙げた相手がいれば、同じセッションでランタイ
ムが所有することになるテーブルを見せておく(起動時のマイグレーションはサ
ービスアカウントとしてテーブルを作るので、所有権はデプロイした人ではなく
書いた人に従う):

```sql
GRANT "<database_user output>" TO "you@your-org.example";
```

それ以降は `cloud-sql-proxy --auto-iam-authn` と自分自身の identity で接
続する — こちらの経路にもパスワードは無い。

## オプション

| 変数 | ガイド | 効果 |
|---|---|---|
| `enable_private_ip` | §2b | Cloud SQL の公開エンドポイントを落とす。peering range と Cloud Run 側の Direct VPC egress。無料。ローカルの `cloud-sql-proxy` アクセスには、その後 VPC に接続したワークステーションか一時的なパブリック IP が要る。 |
| `enable_vertex_embeddings` | §4 | **既定で on**: `roles/aiplatform.user` を付与し `aiplatform.googleapis.com` を有効化する。on にすると何が手に入るか、デプロイ後に何をすべきかは[デプロイガイド](../cloudrun/README.md#4-hybrid-semantic-search-vertex-ai-on-by-default)にある。`false` にすると `OCHAKAI_EMBEDDINGS=off` を渡し、何も付与しない。 |
| `embedding_model` | §4 | 既定は null — ochakai が自分のプロジェクトと製品既定のモデルを使う。モデル・リージョン・プロジェクトを選ぶなら Vertex AI のモデル resource name をそのまま渡す(`OCHAKAI_EMBEDDINGS` はこの一つで三つを運ぶ、設計ドキュメント 0078)。名指したデプロイは semantic search を**要求する**ので、pgvector の bootstrap を先に済ませておくこと。次元は設定できない — モデルのものであってデプロイのものではない。 |
| `enable_gcs_attachments` | §4b | バケット + `roles/storage.objectUser` + `OCHAKAI_GCS_BUCKET`。無いと attach 操作は 501 を返す。 |
| `enable_webui` | §5b | `serve-ui` を、同じイメージ・専用の identity で、IAP の背後にある二つ目の Cloud Run サービスとして立てる。`webui_records_browser_user`(既定 on)は、UI のサービスアカウントではなくブラウザの本人を記録する。 |
| `read_only` | §5d | `OCHAKAI_MODE=read-only` を設定する — 非公開のままである。on にする*前*にベースへ import しておくこと: read-only なデプロイには import できない。 |
| `public_read_only` | §5d | `OCHAKAI_MODE=public` を設定し、このモジュール唯一の `allUsers` を付与する。`read_only` を含意する。その姿勢が何を手放すかは[デプロイガイド](../cloudrun/README.md#5d-optional-a-public-read-only-demo-the-one-public-posture)にある。書き込み可能な状態で apply し、`ochakai import examples/demo` した後、これを on にして再度 apply する。 |
| `maintenance_users` | §6 | 人のための Cloud SQL IAM ログイン、パスワードは無い。 |
| `database_backups` | §6 | 日次バックアップ。コストのため既定は off。大事なものには on にする。 |

web UI を有効にすると `google-beta` provider が付いてくる: Cloud Run 上の
IAP(`iap_enabled`)はまだベータの面だからである。provider はどちらにせよ
宣言されているので、UI が off のときも download はされる — その場合は何
もしない。

## このモジュールが扱わないもの

意図してのことである。それぞれ文書化されている — ほとんどはガイドに、
hardening とアップグレードは
[運用ガイド](../../docs/guides/operating.md)にある。

- **スキーマ bootstrap SQL**(ガイド §3)— データベースのパスワードが要
  る。上を見よ。
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

attachments が有効なら、バケットはオブジェクトを持っている間 destroy を
拒む — それは誰かが添付したすべてのファイルの、唯一のコピーだからである。
本気でやるときは、同じ `apply` に `-var gcs_force_destroy=true` を足す。

有効にした API は有効なままにする: 一つを off にするのはプロジェクト全体
に及ぶ行為であり、他の何かがそれに依存しているかもしれない。
