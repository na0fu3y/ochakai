# ochakai 設計ドキュメント 0010: MCP OAuth コネクタサービス

Status: Proposed
Date: 2026-07-18

## 1. 目的

[0002](0002-authn-authz.md) §4 で「作らない、必要になったら」と保留した
MCP OAuth(Issue #5)を再評価し(Issue #44)、**作る**と決定する。
ただし本体の姿勢は変えない: OAuth は認可機構ではなく、
**claude.ai / ChatGPT リモートコネクタという新しい経路のための
「到達制御 + provenance」の別ドア**として、分離した公開サービスに実装する。

0002 §4 の第一項(MCP OAuth)を本ドキュメントが supersede する。
0002 の原則(到達できた者は読み書きできる、ochakai は認可を持たない)は
維持する。role / スコープは今回も作らない。

## 2. 再評価: #5 / 0002 の時点から変わったこと

### 2.1 DCR 問題の消滅(最大のコスト要因が落ちた)

#5 が「最小 AS を自前で持つ」設計を重くしていたのは、Google が dynamic client
registration(RFC 7591)を持たないため、ochakai 自身が**書き込み可能な公開
registration エンドポイントとクライアント登録ストア**を運用する必要があった点。

MCP authorization 仕様 2025-11-25 は **Client ID Metadata Documents(CIMD、
SEP-991)を SHOULD、DCR を MAY** に変えた。CIMD では `client_id` がクライアント
自身のホストする HTTPS URL で、AS はそれを fetch して metadata を得る —
**registration エンドポイントも登録ストアも不要**になる。

- claude.ai(Web / Desktop / mobile / Cowork)と Claude Code は CIMD 対応済み。
  Claude Code は自身の CIMD document を公開しており、loopback redirect で接続する
- ChatGPT のカスタムコネクタも CIMD(public client、`token_endpoint_auth_methods`
  `none`)対応済み
- claude.ai のカスタムコネクタは事前登録クレデンシャルの手入力にも対応
  (組織管理者が client_id/secret を入力)。CIMD があれば不要だが、退路がある

### 2.2 経路の必要性が具体化した

claude.ai / ChatGPT の Web コネクタは Anthropic / OpenAI のクラウドから接続する
ため、**公開到達可能 + OAuth** が必須で、現在の ochakai(gcloud proxy or SA 前提、
非公開 IAM)はこの経路から原理的に到達できない。MCP はリモート接続が主戦場に
なり(Linux Foundation 移管、主要ベンダー採用)、競合はリモート前提で MCP
サーバーを出している。「チャット UI から自分の組織のナレッジを引く」体験は
この経路でしか提供できない。

### 2.3 代替案の不成立を確認(Issue #44 の宿題)

| 代替案 | 結論 |
|---|---|
| IAP + Workforce Identity | 不成立。IAP は pre-auth レイヤーとしてリクエストを横取りするため MCP の OAuth discovery / DCR / CIMD フローと非互換。GCP にマネージド MCP ゲートウェイは存在しない(2026-03 時点) |
| claude.ai `static_headers`(beta) | 不成立。静的ヘッダでは Cloud Run IAM(Google 署名 ID トークン、1h 失効)を通れない。組織共有クレデンシャルになり per-user provenance も失われる |
| Google をそのまま AS にする(自前 AS なし) | 不採用。PRM で `accounts.google.com` を指す構成は動くが、(1) Google の refresh token は非標準の `access_type=offline` パラメータ必須で MCP クライアントは送らない → **1 時間ごとに対話的再認証**、(2) RFC 8707 resource indicator 非対応でトークンが ochakai に audience 束縛されない、(3) opaque access token の検証が tokeninfo 往復になる |

結論: **最小 AS を自前で持つ(Google に login を委譲)以外に経路がなく、
その最小 AS は CIMD により #5 想定より大幅に小さくなった。**

## 3. 決定

### 3.1 分離した「コネクタサービス」として作る

Cloud Run の IAM invoker チェックは**サービス単位**でパス単位にできないため、
「公開 /mcp + 非公開 その他」は 1 サービスでは実現できない。そこで:

- **本体サービス(非公開・IAM)は無変更。** `httpauth` の「非公開前提・
  署名非検証」という内部不変条件も触らない
- **同一イメージを第二の Cloud Run サービス(公開、`--allow-unauthenticated`)
  としてデプロイ**し、同じ Cloud SQL を共有する。コネクタモードでは
  `/mcp` + OAuth エンドポイント + well-known **のみ**を公開し、
  `/api/v1` / web UI は 404(攻撃面の最小化)
- コネクタサービスの認証は `httpauth` ではなく新パッケージ(仮称
  `connectorauth`)が行う: **署名検証を含む完全検証**。二経路は
  コードレベルで分離され、取り違えは構成上起こらない

デプロイは**オプトイン**: コネクタ経路が要らない組織は今まで通り
非公開サービスだけを運用する。公開するか否かはデプロイ判断であり、
既定の姿勢(非公開 IAM)は変わらない。追加コストは Cloud Run 1 サービス分
(request-based、アイドル時 ~$0)。

### 3.2 二層整理は崩れない

| 層 | 本体サービス | コネクタサービス |
|---|---|---|
| 到達制御 | Cloud Run IAM | ochakai 発行の OAuth トークン検証 |
| provenance | IAM 通過済み ID トークンのクレーム | トークンに束縛された Google 検証済み email |

OAuth は「Google ID トークンを対話的に取得・更新する経路」(Issue #44)であり、
認可ではない。**到達できた者は読み書きできる**は両経路で同じ。スコープは
定義しない(`openid email offline_access` は接続・refresh のためだけに使う)。

## 4. 設計

### 4.1 OAuth エンドポイント(コネクタモードのみ)

- `GET /.well-known/oauth-protected-resource`(および
  `/.well-known/oauth-protected-resource/mcp`、RFC 9728):
  `resource` = `https://<connector-host>/mcp`(ユーザーが入力する URL と
  完全一致必須)、`authorization_servers` = [自分自身]
- 未認証の `/mcp` には **401 +
  `WWW-Authenticate: Bearer resource_metadata="…"`** を返す(claude.ai は
  200 上のこのヘッダを無視する。401 が必須)
- `GET /.well-known/oauth-authorization-server`(RFC 8414):
  `code_challenge_methods_supported: ["S256"]`、
  `client_id_metadata_document_supported: true`、
  `token_endpoint_auth_methods_supported: ["none"]`、
  `grant_types_supported: ["authorization_code", "refresh_token"]`、
  `scopes_supported: ["openid", "email", "offline_access"]`
  (`offline_access` の広告が refresh token 要求のトリガー — SEP-2207)
- `GET /oauth/authorize`: `client_id` は CIMD URL。検証してから
  **同意ページ**(client_name と redirect_uri を明示 — MCP 仕様の loopback
  偽装対策)を挟み、Google へリダイレクト(組織が用意した単一の Google OAuth
  クライアント、consent screen は Internal)
- `GET /oauth/callback`: Google `id_token` を **JWKS で署名検証**し、
  `email_verified` と **`hd` クレーム == 設定ドメイン**を強制(consent screen
  Internal と合わせて二重の組織チェック)。authorization code を発行
- `POST /oauth/token`(`application/x-www-form-urlencoded` 必須):
  - `authorization_code`: PKCE S256 検証 → access + refresh token 発行
  - `refresh_token`: **ローテーション**(旧 refresh を失効させ、同一
    レスポンスで新 refresh を返す — public client の OAuth 2.1 要件)。
    無効な refresh には RFC 6749 準拠の `invalid_grant`
  - `resource` パラメータ(RFC 8707)が来たら自 MCP URL と一致することを検証
- 応答遅延: claude.ai は discovery / token に 10 秒でタイムアウトする。
  全エンドポイントを DB 1 往復以内に収める

### 4.2 CIMD の取り扱い

`client_id` URL の fetch は SSRF になり得るため:

- https のみ、DNS 解決後の IP がプライベート/リンクローカルなら拒否、
  リダイレクト追跡は同一制約で最大 1 回、サイズ・時間上限、
  `application/json` のみ受理
- `redirect_uri` は CIMD document の `redirect_uris` と完全一致。ただし
  loopback(`127.0.0.1` / `localhost`)は **RFC 8252 §7.3 に従いポート無視**で
  照合する(Claude Code の CIMD document は `http://localhost/callback` と
  `http://127.0.0.1/callback` を宣言し、実際は ephemeral port で待つ)

### 4.3 トークンと永続化

- access / refresh とも **opaque なランダム値(256bit)**。JWT にしない:
  即時失効可能・実装が単純・検証は DB 1 引きで足りる
- DB には **SHA-256 ハッシュのみ**保存。平文はどこにも残らない
- TTL: access 1h、refresh 30 日(絶対期限、ローテーションで延長しない)
- grant 行が `actor_email`(Google 検証済み)を持ち、ミドルウェアは
  Bearer → grant → `human:<email>` で actor 解決。actor の kind は常に
  human(エージェントは従来通り SA + IAM 経路。この経路に agent はいない)
- authorization code / 認可中の pending state も Postgres に置く
  (単回使用、10 分 TTL)。Cloud Run の複数インスタンスとローリング
  デプロイをまたいで安全にするため、インメモリ状態を持たない

### 4.4 設定

| 変数 | 意味 |
|---|---|
| `OCHAKAI_CONNECTOR_PUBLIC_URL` | 設定するとコネクタモードで起動(issuer / resource の基底 URL)。`/api/v1`・web UI は無効化 |
| `OCHAKAI_CONNECTOR_GOOGLE_CLIENT_ID` / `_SECRET` | Google へのログイン委譲に使う組織の OAuth クライアント |
| `OCHAKAI_CONNECTOR_ALLOWED_DOMAIN` | `hd` クレームに強制する Workspace ドメイン |

コネクタモードと `OCHAKAI_INSECURE_DEV` は排他(起動拒否)。

### 4.5 経路まとめ(0002 §3 の更新)

| 経路 | 構成 | actor |
|---|---|---|
| Claude Code / MCP(組織内) | gcloud proxy → 本体(cloudrun-iam) | human:本人メール |
| ヘッドレスエージェント | 専用 SA → 本体(cloudrun-iam) | agent:SA メール |
| **claude.ai / ChatGPT コネクタ** | **コネクタサービス(OAuth + CIMD)** | **human:本人メール** |
| **Claude Code(proxy なし直結)** | **コネクタサービス(CIMD + loopback)** | **human:本人メール** |

Claude Code の proxy なし直結(#5 の元々の動機)がコネクタサービスの
副産物として手に入る。

## 5. セキュリティ(SECURITY.md の更新を伴う)

- 「サービスが非公開であることが絶対条件」の文言を二経路に更新する:
  本体サービスは従来通り非公開絶対。コネクタサービスは公開だが、
  署名検証済みトークン以外で actor が立つ経路はない
- 公開化で増える面: token エンドポイントへの総当たり(レート制限、
  opaque 256bit で実質不可)、CIMD fetch の SSRF(§4.2)、open redirect
  (redirect_uri 完全一致で遮断)、DoS(Cloud Run `max-instances`、必要なら
  Cloud Armor)。PKCE downgrade は S256 以外を拒否して遮断
- prompt injection / tool poisoning(MCP 系の既知の攻撃面): 公開化で
  「非認証の外部者が知識を汚染する」経路は**増えない** — 書き込みは
  両経路とも組織認証の通った actor だけで、コネクタ経路も `hd` 強制後のみ。
  組織内 actor による汚染は従来から provenance(記録)で追跡する整理のまま
- 脅威モデル上とくに歓迎する報告: `hd` 強制の迂回、CIMD 検証の迂回、
  token ローテーションの競合悪用、コネクタモードで `/api/v1` 等に
  到達する方法

## 6. 段階と作らないもの

実装は 2 段階(いずれも v0.4 目標、合計 ~1,000–1,500 行 + テスト):

1. **コア**: connectorauth ミドルウェア、トークンストア、`/oauth/token`、
   well-known 2 種、401 ハンドシェイク
2. **認可フロー**: `/oauth/authorize` + CIMD 検証 + 同意ページ + Google
   callback(JWKS 検証)、deploy/cloudrun README の §追加、SECURITY.md 更新

作らないもの(発動条件つきで保留):

- **DCR(RFC 7591)**: CIMD で claude.ai / ChatGPT とも足りる。CIMD 非対応の
  クライアントからの接続要望が具体化したら
- **`oauth_anthropic_creds` / ディレクトリ掲載**: ochakai を Claude の
  コネクタディレクトリに載せる話が出たら
- **Enterprise Managed Auth(SSO assertion)**: エンタープライズ組織からの
  要望が出たら
- **スコープ / 認可**: 従来通り。「読めるが書けない人」が必要になったら
  0002 を改訂する

## 7. 参考

- Issue #44(再評価)、Issue #5(元スケッチ、NOT_PLANNED)
- MCP authorization 仕様 2025-11-25:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- claude.ai コネクタ認証ドキュメント(CIMD 選択条件、offline_access、
  タイムアウト、callback URL):
  https://claude.com/docs/connectors/building/authentication
- CIMD(SEP-991 / draft-ietf-oauth-client-id-metadata-document)
- RFC 9728(protected resource metadata)、RFC 8414(AS metadata)、
  RFC 8252 §7.3(loopback redirect)、RFC 8707(resource indicators)
