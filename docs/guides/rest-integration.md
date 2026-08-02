# REST API を自分のサービスに組み込む

ochakai の REST API は、CLI や MCP クライアントからだけでなく、あなた
自身の Web サービス — データ分析のチャットアプリや、ブラウザの
フロントエンドを持つ内部エージェント — から直接呼ばれることを想定した
小さな面である。[OpenAPI ファイル](../../api/openapi.yaml)一つがその
契約のすべてであり、このページはそのスキーマだけでは語られないそれ
以外のすべて — どう認証するか、あなたの製品の利用者全員を一つの
identity に潰さない方法、そして他の書き手と競合せずに書く方法 — を
扱う。

ochakai 自体をデプロイしようとしていて、すでに動いているものに対して
組み込もうとしているのでなければ、代わりに
[デプロイガイド](../../deploy/cloudrun/README.md)から始めるとよい。

**REST は `/api/v1` で凍結されている**(設計ドキュメント
[0064](../design/0064-rest-stops-at-api-v1.md)): このページが説明する
契約は、セキュリティ修正を除いて動かない — マイナーリリースでも変わり
うる MCP・CLI・保存形式とは違う。完全な方針は
[docs/compatibility.md](../compatibility.md)にある。

## 認証する

Cloud Run IAM が ochakai に届く者を決め(設計ドキュメント
[0002](../design/0002-authn-authz.md)、
[0003](../design/0003-gcp-only.md)) — ochakai 自身が発行したり検証したり
する API キーやベアラートークンは無い。すべてのリクエストは、あなたの
ochakai サービスの URL に audience が紐づいた Google 署名の **ID
トークン**を `Authorization: Bearer <token>` として運ぶ。Cloud Run は
リクエストがコンテナに届く前にそれを検証し、ochakai は転送されてきた
identity を provenance の記録にのみ使う。

あなたのサービスアカウントには、ochakai サービスへの
`roles/run.invoker` が要る:

```sh
gcloud run services add-iam-policy-binding ochakai \
  --region="$REGION" \
  --member="serviceAccount:$YOUR_SERVICE_ACCOUNT" \
  --role=roles/run.invoker
```

シェルからは、すでに持っている CLI でトークンを発行する:

```sh
TOKEN=$(gcloud auth print-identity-token --audiences="$OCHAKAI_URL")
curl -H "Authorization: Bearer $TOKEN" "$OCHAKAI_URL/api/v1/search?q=revenue"
```

あなた自身のサービスからは、シェルアウトせずクライアントライブラリで
発行する — 主要な Google Cloud クライアントライブラリはどれも ID
トークンのヘルパーを持つ。Python の例:

```python
import google.auth.transport.requests
import google.oauth2.id_token

request = google.auth.transport.requests.Request()
token = google.oauth2.id_token.fetch_id_token(request, audience=OCHAKAI_URL)
```

[`examples/bigquery-catalog`](../../examples/bigquery-catalog/bundle/jobs/sync-bigquery-catalog/sync-bigquery-catalog.py)
はこれの完全に動く例である — Application Default Credentials で
BigQuery を読み、発行した ID トークンで ochakai に書き込む。自前の
秘密は一つも持たない。

<a id="delegated-provenance-forwarding-who-used-your-product"></a>

## 委譲された provenance: あなたの製品を使った人を転送する

これが無いと、あなたの利用者が書くすべての concept は、あなたの
アプリケーションの一つのサービスアカウント identity の下に記録され、
provenance — ochakai が売るものの大半 — が一つの名前に潰れる。

あなたのアプリケーションが利用者に代わって identity を転送すること
を ochakai に伝える(設計ドキュメント
[0027](../design/0027-delegated-provenance.md))。記録されるのは常に
両方の identity であり — `human:tanaka@… via process:app-sa@…` —
転送された側だけになることは無い。だから、あなたのアプリケーション
を通した書き込みは、その人が直接行った書き込みと区別できるままに
なる。

```sh
# roles/run.invoker(認証する、前述)に加えて: あなたのアプリケーション
# の identity だけに、利用者の代わりに話すことを許可する。
gcloud run services update ochakai --region="$REGION" \
  --update-env-vars="OCHAKAI_DELEGATING_CALLERS=$YOUR_SERVICE_ACCOUNT"
```

各リクエストにこのヘッダーを付ける:

```
Ochakai-On-Behalf-Of: human:tanaka@example.co.jp
```

種別(`human:` / `process:`)は必須で、推測されることは無い —
転送しているのが人なのか別のソフトウェアなのかは、あなたの
アプリケーションが知っている。

注記:

- **委譲は既定で off である。** `OCHAKAI_DELEGATING_CALLERS` が未設定
  だと、このヘッダーは静かにダウングレードされるのではなく **403**
  になる — 利用者として書き込んでいるつもりのアプリケーションが、
  何か月も経ってからすべての concept がそう言っていないと気づく、
  ということがあってはならない。
- **このヘッダーに言及する 403 は、呼び出し元が一覧に無いことを
  意味する。** `run.invoker` を付与したサービスアカウント(認証する、
  前述)とここの値を見比べるとよい — 同じメールアドレスであるはずで、
  食い違いがたいていの原因である。**400** はヘッダー自体が不正な形で
  ある(種別が無い、identity に空白が入っている)ことを意味する。
- `OCHAKAI_DELEGATING_CALLERS` はカンマ区切りの一覧を取る。`*` は
  認証済みの呼び出し元すべてを信頼する — IAM がすでに自分の
  アプリケーション以外を通していない場合にのみ理にかなう。
- **これは認可ではない。** 決めるのは誰の identity が記録されるかで
  あって、誰が何をしてよいかではない — ochakai に届く呼び出し元は
  誰でもすでにすべてを読み書きできる(設計ドキュメント
  [0002](../design/0002-authn-authz.md))。到達性は引き続き IAM の
  仕事である。

## 何が書いたかを宣言する: Ochakai-Producer

それを保証する identity の隣に、あなたのソフトウェアの名前を置く
(設計ドキュメント
[0052](../design/0052-producer-beside-the-actor.md)):

```
Ochakai-Producer: insightflow/1.4.0
```

スラッシュ一つ、両側とも空でない `<producer>/<version>` の形。
`Actor.producer` として、認証された行為者の隣に記録され、決してその
代わりにはならない(`human:tanaka@… using insightflow/1.4.0`)。だから、
あなたの製品を通して書かれた concept は、それでも誰が書いたかを言い
続ける。許可リストで絞られることは無い — 名乗るのはあなた自身の
ビルドであって他人の identity ではないからである。不正な値は 400 に
なる。

## 同時書き込み: ETag と If-Match

より良い方法を求めない限り、最後に書いた者が勝つ。
`/api/v1/bundle/{path}` への読み書きはすべて `ETag` を返す — concept
の正規の OKF ドキュメントのハッシュを引用符付きで、本文にも
`summary.content_hash` として返す — そして、古い値を持った
`If-Match` を添えた `PUT` は `412` になり、何も書き込まない
(設計ドキュメント [0030](../design/0030-optimistic-locking.md)、
0043 §3.4):

```sh
etag=$(curl -si "$OCHAKAI_URL/api/v1/bundle/metrics/revenue.md" \
  -H "Authorization: Bearer $TOKEN" | grep -i '^etag:' | cut -d' ' -f2 | tr -d '\r')
curl -X PUT "$OCHAKAI_URL/api/v1/bundle/metrics/revenue.md" \
  -H "Authorization: Bearer $TOKEN" -H "If-Match: $etag" \
  -H "Content-Type: text/markdown" --data-binary @revenue.md
```

これは内容だけのハッシュなので、concept を検証する・却下する・
ファイルを添付するだけでは、保持している precondition は有効なまま
である — 無効にするのは編集だけである。

## ローカルでの開発

`deploy/compose.yaml` と `OCHAKAI_MODE=dev` はどちらも認証を off に
する: 呼び出し元は全員 `human:anonymous` になるので、不正な形の
`Ochakai-On-Behalf-Of` や `Ochakai-Producer` ヘッダーは、Cloud Run
への最初のデプロイではなく、あなたの手元で失敗する。
`OCHAKAI_MODE` の他の姿勢については
[要件と設定](../configuration.md#environment-variables)を見よ。

## 関連ページ

- [api/openapi.yaml](../../api/openapi.yaml) — 検査される契約: すべて
  のリクエストとレスポンスは CI でこれと照合される。
- [要件と設定](../configuration.md) — 上で名前を挙げたすべての環境
  変数。
- [examples/bigquery-catalog](../../examples/bigquery-catalog) —
  完全なアプリケーション形の統合例: ウェアハウスを読み、ochakai に
  書き込み、秘密は一つも持たない。
