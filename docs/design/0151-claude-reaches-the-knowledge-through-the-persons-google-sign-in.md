# ochakai 設計ドキュメント 0151: Claude は、本人の Google のサインインでナレッジに届く

Status: Accepted(2026-09-26)。[0116](0116-the-connector-price-changed-not-its-condition.md)
を Superseded にする — 0116 が置いた再開の条件が満たされたので、出発点
(RFC 9728 の二つの答え)を採用する。[0070](0070-what-was-retired-and-why.md)
§2・§5 を改訂する — 撤去したのは ochakai が認可サーバになる形であって、
呼び出し元が自分でトークンを取りに行ける形ではない。secret-zero
([0065](0065-identity-and-provenance.md)・[0086](0086-a-second-way-to-say-who-is-calling.md))
も、認可を持たないこと(0065 §1)も動かない。ROADMAP のデータエージェントの
第四段「人がもう問うている場所へ」
Date: 2026-09-26

## 0. この記録が決めたこと

1. **自分で検証するデプロイは、クライアントに認証の行き先を教える。**
   `/mcp` の 401 に `WWW-Authenticate` で RFC 9728 のメタデータを名指し、
   メタデータが発行者を名指す(§2)。
2. **Google Workspace を発行者にできる。** Google のアクセストークンは JWT で
   なく、中身は Google にしか読めない。発行者が Google のデプロイは、
   JWT でないトークンを Google の tokeninfo に問い、この OAuth クライアントに
   発行されたこと・期限内であること・確かめられた email を確かめる(§3)。
3. **ochakai は secret を持たない。** OAuth クライアントの secret は、
   Claude の組織設定に管理者が入れる。ochakai はトークンを確かめるだけ
   である(§4)。
4. **答えるのは Claude で、BigQuery も Claude が本人として走らせる。**
   ochakai はナレッジを渡し、デプロイ自身のエージェントは MCP に載らない
   ままである(§5)。

## 1. 誰が詰まったか、なぜいまか

ochakai を入れている会社の人たちは Google Chat・Claude・Slack で問う
(2026-09-26、運用者の報告)。0116 §7 は「claude.ai / ChatGPT の Web
コネクタから組織ナレッジを引きたい利用者が具体化したとき」を再開の条件に
した。**その条件が満たされた。**

Google Chat は橋を作って実機で動かし、撤退した(ROADMAP)。Chat の
アプリには本人のトークンが届かず、本人として SQL を走らせられなかった
からである。**本人の身元を既に持っているアシスタントに問わせる**のが残る
道で、Claude がそれである — Claude は BigQuery を自分のコネクタで本人と
して走らせ、Slack からも使われる。

## 2. 握手(0116 §1 の二つの答え)

- `GET /.well-known/oauth-protected-resource/mcp` — RFC 9728 のメタデータ。
  `authorization_servers` は `OCHAKAI_OIDC_ISSUER`。
- `/mcp` の 401 に `WWW-Authenticate: Bearer resource_metadata="…"`。
  `/api/v1` の 401 には付けない(凍結された契約の宣言された応答だから、
  0116 §5)。

`resource` は、audience が https の URL ならそれを、そうでなければ(Google
のように audience が OAuth クライアント ID なら)**リクエストが届いた
ホストの `/mcp`** を名乗る。Claude は `resource` が利用者の入れた URL と
一字一句同じであることを求めるので、Cloud Run の二つの URL のどちらで
入れても合う。

## 3. Google のアクセストークンを確かめる

Google で人をサインインさせた OAuth クライアントが得るのは opaque な
アクセストークンで、署名を確かめる手段が手元に無い。発行者が
`https://accounts.google.com` のデプロイは、JWT でないトークンを Google の
tokeninfo に問う。

- **この OAuth クライアントに発行されたこと**(`aud` か `azp` が
  `OCHAKAI_OIDC_AUDIENCE`)。無ければ、Google が**誰かの別のアプリ**に
  発行したトークンが通る — 0086 §2 が断る confused deputy である。
- **期限内であること**、**確かめられた email**(無ければ 0117 のとおり
  process として記録し、そう言う)。
- **トークンは POST の本文で送り、URL に載せない**(プロキシとログが
  URL を残す)。
- **答えは最長 5 分だけ覚える。** トークンは 1 時間生きるが、取り消された
  人が数分で届かなくなるほうを選ぶ。
- メタデータは `scopes_supported: ["openid", "email"]`、チャレンジも
  `scope="openid email"` を言う。email の scope が無いと、tokeninfo は
  誰のトークンかを言わない。**身元の scope であって、権限の scope では
  ない**(0065 §1)。

Google の ID トークン(JWT)は、これまでどおり 0086 の経路で確かめる。

## 4. secret はどこにも増えない

| 要るもの | どこにあるか |
|---|---|
| OAuth クライアントの secret | Claude の組織設定(カスタムコネクタの詳細設定に管理者が入れる)。ochakai は持たない |
| トークンを確かめること | Google の tokeninfo と公開鍵。ochakai が発行するもの、回すものは無い |
| 組織の人だけであること | OAuth 同意画面の種類を「内部」にする — その Workspace の人しかトークンを得られない |

**ochakai が認可サーバになる形は採らない**(0070 §2 が撤去したもの)。
Google を上流にした認可サーバを ochakai が持てば、Google の client secret を
ochakai が持つ。

## 5. 答えるのは Claude である

- **Claude は MCP の 6 本でナレッジを読む**(`search_concepts` → `get_concept`、
  `linked_from`)。書き戻しは `put_concept` の draft と `report_outcome` で、
  人が裁定する(0145 §6)。
- **BigQuery は Claude が自分のコネクタで本人として走らせる。** サーバーは
  SQL を実行しない(0142 §4)。
- **デプロイ自身のエージェントは MCP に載せない**(0145 §5.1)。Claude が
  既にエージェントである。

## 6. 代金

- **公開到達。** Claude は Anthropic の基盤から呼ぶので、Cloud Run は
  `allUsers` の invoker を持つ。**どの呼び出しも、発行者が請け合い、この
  プロセスが確かめたトークンを要る** — 無ければ 401 である。デプロイ
  ガイドの「書けるデプロイに `allUsers` は決してだめ」は「自分で検証する
  デプロイだけ」に変わる。入り口を Anthropic の送信元(公開されている範囲)
  に絞るのは、ロードバランサと Cloud Armor の仕事で、ochakai の中には
  持たない(ROADMAP のレート制限と同じ線)。
- **`OCHAKAI_DELEGATING_CALLERS=*` は起動を止める**(0116 §5)。自分で
  検証するデプロイでは、検証された誰もが任意の人物を名乗れることになる。
- **断った呼び出しの理由をログに残す**(トークンは載せない)。クライアントが
  本文ではなく読み込み中の表示を見せる以上、運用者が読めるのはログだけで
  ある。
- **発行者の側の代金**(0116 §4)は残る: クライアントを事前登録すること
  (Google は動的登録をしない)、email の scope を与えること。

## 7. 実機で確かめたこと(2026-09-26)

公開到達・read-only の試行サービスを `ochakai-example` に立て、Google の
OAuth クライアント(内部)で claude.ai のカスタムコネクタを足した。

- 401 → メタデータ → Google のサインイン → **Claude が Google のアクセス
  トークンで `/mcp` を呼び、tokeninfo で確かめられ、`human` として 200** に
  なった。
- 別のクライアント(gcloud)に発行された本物の Google トークンは、
  「別の OAuth クライアントに発行された」として 401 になった。
- 最初の二度は、Google のサインインのあと Claude が戻ってこなかった
  (ochakai には一度もトークンが届かなかった)。**コネクタを消して名前を
  変えて足し直すと通った** — Claude の側で、失敗した接続の状態が残る
  ことがある。デプロイガイドに書く。

## 8. 面

- **REST・MCP・CLI・環境変数** — どの数も動かない。使うのは既存の
  `OCHAKAI_OIDC_ISSUER`・`OCHAKAI_OIDC_AUDIENCE` だけである。
- `/.well-known/oauth-protected-resource/mcp` は `/api/v1` の外の住所で、
  `WWW-Authenticate` は `/mcp` の応答にだけ立つ。[docs/surface.md](../surface.md)
  の「数えていないもの」に並べる。

## 9. 退けた案

- **ochakai を Google の前に立つ認可サーバにする**(0010 の形)。Google の
  client secret を ochakai が持つ(C2)。
- **Claude の `static_headers`**(管理者が入れる固定のトークン)。共有の
  secret で、全員が一つの名前に潰れる。
- **aud を確かめずに Google のトークンを受ける。** 誰の別のアプリの
  トークンでも通る(§3)。
- **tokeninfo の答えを 1 時間覚える。** 取り消しが 1 時間効かない(§3)。
- **Google Chat・Slack の橋。** ROADMAP の「やらないこと」。
