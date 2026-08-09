# 要件と設定

<a id="requirements"></a>

## 要件

**本番で動かすとは Google Cloud で動かすことである。** デプロイに誰が
届くかは Cloud Run の IAM チェックが決め、Cloud SQL はサービスアカウント
を認証する — これが ochakai が自前の秘密を一つも持たない理由であり、
他のクラウドやオンプレミスで動かす手段をサポートしない理由でもある
(設計ドキュメント [0065](design/0065-identity-and-provenance.md) §1、
[0003](design/0003-gcp-only.md))。持ち運べるのはナレッジであって、
ランタイムではない — OKF バンドルとして往復するので、離れることは何から
離れるかに依存しない。`deploy/compose.yaml` は同じバイナリを認証オフで
ローカルに動かす — 本番の縮小版ではなく、開発用のハーネスである。

**認可は無い。** 到達できるかどうかがアクセスモデルのすべてである —
下の[認証に設定はない](#authentication-has-no-configuration)を見よ。

**PostgreSQL、それに `pg_trgm`。** 最初のマイグレーションがこの拡張を
作るので、`CREATE EXTENSION` の権限を持たないユーザーで動かすデータ
ベースは、管理者が一度だけ作っておく必要がある — deploy ガイドの
bootstrap SQL はそのためにある。セマンティック検索は同じ要領で `vector`
(pgvector)を足す — Google Cloud 上では既定で有効であり、ベクトルを
持てないデータベースは、検出されたデプロイであれば失敗ではなく字句検索
へフォールバックする。それが無くてもプレーンな PostgreSQL で足りる。
CI と deploy ガイドはどちらも Postgres 17 で動かしている。

**サーバーには Docker が要る。クライアントのコマンドにはバイナリが
要る。** README のクイックスタートは唯一の CLI コマンド `import` を
compose のコンテナの中で動かす
(`docker compose exec ochakai /ochakai import ...`)ので、それをなぞる
だけなら Docker 以外は要らない。その先 — `ochakai use`・
`ochakai context`・web UI — には
[リリースアーカイブ](https://github.com/na0fu3y/ochakai/releases)か、
`go run ./cmd/ochakai`(`go.mod` が名指す toolchain が要る。Go 1.21
以降ならそれを自動で取ってくる)を使う。あるいは API を直接叩いても
よい — CLI がやっているのはそれだけなので、一行の `curl` で書き込む版は
[データモデル](architecture.md#the-data-model)にある。

<a id="environment-variables"></a>

## 環境変数

| 環境変数 | 説明 |
|---|---|
| `OCHAKAI_DATABASE_URL` | Cloud SQL の接続文字列(必須) |
| `OCHAKAI_DB_IAM_AUTH` | `true` で Cloud SQL IAM データベース認証を有効にする: 接続パスワードは短命の IAM トークンになるので、接続文字列は秘密を運ばない |
| `OCHAKAI_GCS_BUCKET` | ファイルの実体を置くバケット(認証は ADC — キーは無い)。既定: 未設定 — このインスタンスは markdown の concept だけを保存し、ファイルの書き込みはエラーを返す |
| `OCHAKAI_EMBEDDINGS` | このデプロイが**どう埋め込むか**を一語で言う(設計ドキュメント [0080](design/0080-search-and-how-a-deployment-embeds.md))。未設定 / `on`: Google Cloud 上で動いていれば、ochakai はメタデータサーバーから自分の**プロジェクトとリージョン**を読み、製品既定のモデル(`gemini-embedding-001`)で埋め込みを有効にする — 設定することは何も無く、**埋め込みは動いているリージョンで行われる**ので、asia-northeast1 に立てたデプロイの本文と検索クエリは asia-northeast1 の Vertex AI に送られる(設計ドキュメント [0080](design/0080-search-and-how-a-deployment-embeds.md) §1.2)。リージョンが読めなかったときは埋め込みを有効にしない — 誰も選んでいないリージョンにテキストを送るより、字句検索で動くほうを選ぶ。そのリージョンにモデルが無ければ起動時の問い合わせがそう答え、同じく字句検索に落ちる。実際に Vertex AI を呼べるかどうかは設定ではなく IAM(`roles/aiplatform.user`)が決め、起動時に一度だけ問い合わせる(設計ドキュメント [0080](design/0080-search-and-how-a-deployment-embeds.md))。Google Cloud の外にはメタデータサーバーが無く、何も有効にならない。`off`: 放っておけばハイブリッドになるデプロイを字句検索だけで動かす — Vertex AI は一度も呼ばれない。**Vertex AI のモデル resource name**(`projects/<project>/locations/<location>/publishers/google/models/<model>`): そのモデル・リージョン・プロジェクトで埋め込む。画像・PDF のファイル検索には model を `gemini-embedding-2`、location を `global`(または `us`/`eu`)にする。名指したデプロイはセマンティック検索を**要求した**ことになり、Vertex AI か pgvector が無ければ起動を拒否する(検出しただけのデプロイが字句検索へフォールバックするのと対照的である)。ベクトルの次元は設定ではなくモデルごとの定数で(現在はどちらのモデルも 768)、ochakai が知らないモデルは起動エラーにする — 幅を推測すれば、保存済みのベクトルと比べられないものを書くことになる。ベクトルは concept が書かれたときに書かれるので、埋め込みが届く前に読み込んだベースや、model を変えた後のベースは、`ochakai reembed` を走らせるまで埋め込まれないままになる。三つのどれでもない綴りは、推測せず起動エラーにする。既定: 検出できた場所では on |
| `OCHAKAI_DELEGATING_CALLERS` | `Ochakai-On-Behalf-Of: human:tanaka@example.co.jp` でエンドユーザーの identity を転送してよい呼び出し元 identity を、カンマ区切りで並べる(`*` で認証済みの呼び出し元すべて)。ochakai を組み込み、多くの人に使わせるアプリケーションのためのもので — これが無いと、そのアプリの利用者は全員アプリの一つのサービスアカウントに潰れてしまう。記録されるのは常に両方の identity であり(`human:tanaka@… via process:app-sa@…`)、転送された側だけになることは無い。既定: 空、委譲は off。一覧に無い呼び出し元からのヘッダは、黙って無視せず 403 にする(設計ドキュメント [0065](design/0065-identity-and-provenance.md) §3) |
| `OCHAKAI_PRODUCER` | `ochakai` CLI 専用: CLI を動かしているソフトウェアを `<producer>/<version>` の形で名乗る — 例 `claude-code/2026.07`。`Ochakai-Producer` として送られ、行為者の**隣に**記録される(`human:tanaka@… using claude-code/2026.07`)のであって、行為者に取って代わることは無い — CLI をシェルアウトで叩くエージェントが、操作者の名前だけの下に書き込むことをやめさせるためのものである。認証済みの呼び出し元は誰でもこのヘッダを送ってよい — 名乗るのは呼び出し元自身のビルドであって他人の identity ではないので、許可リストで絞る必要が無い — 不正な値は黙って捨てず 400 にする。MCP の面では、ヘッダが無ければ同じ値をクライアントの `initialize` 情報から取る。既定: 未設定、何も宣言しない(設計ドキュメント [0065](design/0065-identity-and-provenance.md) §4) |
| `OCHAKAI_IAP_AUDIENCE` | `ochakai serve-ui` 専用: 検証する IAP JWT の audience。これにより、ブラウザからの編集が `process:<webui-sa>` ではなく、サインインした本人(`human:tanaka@… via process:<webui-sa>`)として記録されるようになる。サーバー側の `OCHAKAI_DELEGATING_CALLERS` に webui のサービスアカウントを入れておく必要がある。設定すると、IAP が署名していないリクエストはサービスアカウントとして記録せず、拒否するようになる。既定: 空、利用者ごとの provenance は off(設計ドキュメント [0065](design/0065-identity-and-provenance.md) §5) |
| `OCHAKAI_MODE` | このデプロイが**何であるか**を一語で言う — 姿勢は排他的なので、綴れるのは一つだけである(設計ドキュメント [0066](design/0066-four-postures-one-word.md) §1)。未設定は通常の姿勢: 誰が届くかは Cloud Run IAM が決め、届いた者は読み書きできる。`read-only` はナレッジを変えずに提供する — すべての書き込みは 403 になり、MCP は書き込み系のツールを一切提供せず、web UI も失敗するだけのボタンを描かなくなる。参照専用のインスタンスや、移行の間ベースを凍結する用途に。これは認可ではない: 呼び出し元を見ておらず、操作者も同じように拒否する(設計ドキュメント [0066](design/0066-four-postures-one-word.md) §2)。`public` は誰でも届いてよいデプロイの姿勢である — デモや、配布用の参照専用コピーに。**identity を一切読まない**: `Authorization` ヘッダは無視され(手前に Cloud Run IAM が無ければ署名を誰も検証していないので、信じれば任意の呼び出し元が任意の人物を名乗れてしまう)、委譲も無視され、呼び出し元は全員 `human:anonymous` になり、誰も拒否されない。read-only を**含む** — 誰が尋ねたかを記録しないことは、何も書き込まれないときにしか正当化できないからである(設計ドキュメント [0066](design/0066-four-postures-one-word.md) §3)。`dev` はローカル開発専用: 認証は off で、すべてが `human:anonymous` として動く — デプロイでは絶対に使わない。利用状況のテレメトリはどの姿勢でも記録される。サーバー自身の観測だからである。これらのどれでもない綴りは、推測せず起動エラーにする。既定: 未設定 |
| `OCHAKAI_RECORD_MISSES` | `false` にすると、何もヒットしなかった検索を ochakai が残さなくなる。miss はここで唯一、呼び出し元がキュレーションしたのではなく打ち込んだものなので、切り替えが用意されている: off にすると `ochakai stats` は `misses -` を返し、ゼロではなく「測っていない」と言う。それ以外のループの計測は変わらない。公開デプロイ(`OCHAKAI_MODE=public`)はどちらにしても何も残さない — identity を読まないので、見知らぬ相手が何を尋ねたかを集めない。生の利用イベントと同じく 180 日保持する(設計ドキュメント [0069](design/0069-the-loop-and-what-measures-it.md) §4)。既定: on |
| `OCHAKAI_OIDC_ISSUER` | Google Cloud の外で**誰が呼んでいるかを言う二つ目の方法**(設計ドキュメント [0086](design/0086-a-second-way-to-say-who-is-calling.md))。OpenID Connect 発行者の URL(`https://` のみ)を書くと、ochakai が Bearer トークンの**署名・発行者・audience・有効期限を自分で検証する** — Cloud Run ではプロセスの手前で IAM が済ませているチェックを、ここでは中で行う。**secret は増えない**: 検証に使うのは発行者が公開している公開鍵であり、共有鍵系のアルゴリズムは受け付けない。`OCHAKAI_OIDC_AUDIENCE` と必ず対で設定する(片方だけは起動時エラー)。`OCHAKAI_MODE=dev` / `public` とは併用できない — どちらも「identity を読まない」と言っている姿勢だからである。既定: 未設定(Cloud Run が検証したものを読む) |
| `OCHAKAI_OIDC_AUDIENCE` | このデプロイが答える `aud` クレーム。省略できないのは、audience を見ない検証が、**同じ発行者が別のサービスのために発行したトークン**を受け入れてしまうからである(`aud` が存在する理由)。記録される名前は、発行者が verified と言った email なら人として、そうでなければ `sub` を process として記録する — 発行者が請け合わないアドレスで provenance を作れば、それは誰でも名乗れる名前になる |
| `PORT` | 待ち受けポート(既定 `8080`) |

クライアント側のコマンドは `OCHAKAI_URL` を読む — 話しかけるサーバーを、
`ochakai use` で選んだものより優先する。残りは
[CLI リファレンス](cli.md)にある。

<a id="authentication-has-no-configuration"></a>

## 認証に設定はない

デプロイに届く者は誰でも、すべてを読み書きできる。concept ごとの権限が
要るなら、これは向いていないツールである — それで何が買えるかは
[README の refusal 表](../README.md#what-it-refuses)にある。

**Google Cloud の外では、認証だけが第二の経路を持つ**(設計ドキュメント
[0086](design/0086-a-second-way-to-say-who-is-calling.md))。
`OCHAKAI_OIDC_ISSUER` と `OCHAKAI_OIDC_AUDIENCE` を設定したデプロイは
トークンを自分で検証するので、`dev`(誰も認証しない)と `read-only`
(誰も認証せず書き込みも断る)の二択ではなくなる。**認可が生まれるわけ
ではない** — 発行者が請け合った相手は、Cloud Run の場合と同じく全部を
読み書きできる。検索は字句のみになる(埋め込みは Vertex AI か、無いか)。

ochakai は、Cloud Run が IAM チェックの後に転送してくる呼び出し元の
identity(人には `human:<email>`、サービスアカウントには
`process:<sa-email>`)を読み、provenance として記録する — それ以外の
用途では参照しない。到達性 — そもそも誰がデプロイに届いてよいかを
決めること — は完全に Cloud Run IAM の仕事である。上の `OCHAKAI_MODE`
は、届いた呼び出し元にできることを絞る(`read-only`)か、誰であるかの
記録自体をやめる(`public`)かのどちらかであり、どちらも認可ではない —
どちらも、尋ねているのが誰かを見ていないからである。

コストを切り詰めたデプロイの完全な手順(月 $10 程度)は
[deploy/cloudrun/README.md](../deploy/cloudrun/README.md) にある。
セキュリティ強化のチェックリスト(org-policy によるガードレール、
private IP、最後のパスワードを退役させる、など)もそこに含まれる。
