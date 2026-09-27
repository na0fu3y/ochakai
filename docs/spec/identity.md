# 認証・identity・認可

ochakai が**誰が呼んでいるかをどう知り、それを何として記録し、何を
させるか**。secret を一つも持たずにそれを行う(C2)。

## 原則

- **記録で担保し、認可で担保しない。** ochakai がヘッダから読むのは
  provenance — 誰として記録するか — である。誰が検証したか・誰が書いたかは
  台帳に必ず残り、**信頼の判断は読む側が provenance を見て行う**。
- **付与が一つも無いデプロイには認可が無い。** 到達できた者は全部を読み
  書きする。誰を到達させるかは Cloud Run IAM かネットワークが決める。
  ディレクトリ単位の付与(下の「認可」)は、置いたときだけ効く。
- **secret を持たない。** トークンを発行も保管もしない。検証に使うのは
  Google の検証済みヘッダか、発行者が公開している公開鍵か、Google の
  tokeninfo だけである。
- **静かに降格しない。** 自称が通らないときは断るか、断れないなら言う。
  黙って別の名前で記録すると、書き手は数か月後に気づく。

## 呼び出し元を知る経路

経路は二つあり、`OCHAKAI_OIDC_ISSUER` を設定したかで決まる。**二つが違う
ヘッダを読むのは意図である。**

### Cloud Run IAM(既定)

Cloud Run の IAM を通ったリクエストには Google が検証した ID トークンが
届く。ochakai は署名を検証し直さない — IAM を通ったことが検証である。
**`X-Serverless-Authorization` を優先して読み、無ければ `Authorization`
を読む。** Google が検証したのは前者だけなので、逆にすると認証済みの
呼び出し元が `Authorization` に他人のトークンを入れてその人として
記録される。email の無いトークンはエラーにする。

**サービスが非公開であることが絶対条件である。** 公開したサービスでは
ヘッダは自称にすぎず、そのための姿勢が `public` と `sandbox` である
(後述)。

### 自分で検証する(OIDC)

`OCHAKAI_OIDC_ISSUER`(`https://` のみ)と `OCHAKAI_OIDC_AUDIENCE` を**両方**
名指したデプロイは、Bearer トークンの署名・発行者・audience・有効期限を
自分で検証する。片方だけは起動エラーである — audience の無い検証は、
同じ発行者が別のサービスに出したトークンを通す(confused deputy)。

- **`Authorization` だけを読む。** 前段の Cloud Run IAM が検証した
  `X-Serverless-Authorization` は、この発行者の鍵では検証できない。それしか
  無いリクエストは、そう書いた 401 で断る。OIDC と Cloud Run IAM を重ねた
  構成(到達は IAM、誰であるかは組織の IdP)はこれで動く。
- **非対称の署名だけを受ける**(RS256/384/512・ES256/384/512)。共有鍵の
  HMAC は secret なので受けない。`none` は論外である。
- **Google を発行者にできる。** `https://accounts.google.com` のデプロイは、
  JWT でない Google のアクセストークンを tokeninfo に **POST の本文で**
  問い、この OAuth クライアントに発行されたこと(`aud` か `azp` が
  audience)・期限内・確かめられた email を確かめる。答えは**最長 5 分**
  覚える。Google の ID トークン(JWT)は通常の OIDC として検証する。
- `dev`・`public`・`sandbox` は誰の identity も読まない姿勢なので、OIDC と
  組み合わせると起動エラーになる。`read-only` とは組み合わせられる。

### MCP クライアントに行き先を教える

自分で検証するデプロイは、`/mcp` の 401 に
`WWW-Authenticate: Bearer resource_metadata="…"` を付け、
`GET /.well-known/oauth-protected-resource/mcp` で RFC 9728 のメタデータを
返す(`authorization_servers` は発行者、`scopes_supported` は
`openid email`)。Claude のカスタムコネクタは、これで本人の Google
Workspace のサインインを経て `/mcp` に届く。OAuth クライアントの secret は
Claude の組織設定に管理者が入れ、ochakai は持たない。**ochakai は認可
サーバにならない。** `/api/v1` の 401 には付けない — 凍結された契約の
応答だからである。

## 記録される名前

actor の綴りは `human:<id>` と `process:<id>` の二つである。

| 届いたもの | 記録 |
|---|---|
| Cloud Run 経路の `*.gserviceaccount.com` | `process:` |
| Cloud Run 経路のそれ以外の email | `human:` |
| OIDC 経路の、発行者が verified と言う email(`email_verified` が無いときも verified とみなす) | `human:<email>` |
| OIDC 経路の、verified でない email | 使わない |
| OIDC 経路の、email が無い / `email_verified: false` | `process:<sub>` |

人の資格情報で動くエージェントは人として記録される — **あなたの鍵で動く
ものはあなたである。** OIDC 経路で email の無いトークンを最初に受けたとき、
プロセスは警告を**一度だけ**ログに出す(`sub` を添え、トークンは出さない)。
人のトークンでも、発行者が access token に email を載せていなければ
この形で届くからである。拒否しないのは、正当な機械のトークンと見分けが
付かないからである。

### 委譲

埋め込みホストは自分のサービスアカウントで到達するので、放っておくと
利用者全員が一つの名前に潰れる。`OCHAKAI_DELEGATING_CALLERS`(カンマ区切り、
`*` は認証済みの全員、**既定は空で委譲無効**)に載った呼び出し元は、
`Ochakai-On-Behalf-Of: <kind>:<identity>` で利用者を名乗れる。

- kind は `human` か `process` で、推測しない。空白を含む・320 バイト超・
  未知の kind は 400。
- **置き換えずに合成する**: `human:tanaka@example.co.jp via
  process:app@proj.iam.gserviceaccount.com`。本人が書いたものとアプリ
  経由のものが区別できない委譲は偽装と同じだからである。
- **載っていない呼び出し元が送れば 403。** 無視しない。
- 委譲された名前のドメインは検証しない。呼び出し元を信頼して載せた以上、
  その先を疑うのは一貫しない。

### producer

「どのソフトウェアが書いたか」は actor の**隣**に置く四つ目の欄
`producer` である(SPEC §7 の `<producer>/<version>`: スラッシュちょうど
一つ、両側非空、空白・制御文字・コロン無し、128 バイト以内。版の綴りは
検査しない)。不正な値は 400。

- REST と MCP は `Ochakai-Producer` ヘッダで、CLI は `OCHAKAI_PRODUCER` を
  同じヘッダに載せる(`whoami` が申告値を出す)。MCP は呼び出しごとに
  読み、initialize の `clientInfo` には頼らない。Web UI には出さない。
- 委譲の後に解決するので、最終的に記録される名前に付く。
- allowlist は持たない — 名乗るのは呼び出し元自身のビルドで、呼び出し元は
  同じ actor の中に認証済みで記録されている。
- 記録先は actor が `via` を持つ場所すべて。検索ミスなどの刈られる
  テレメトリには足さない。取り込みは他インスタンスの producer を読み戻さない。

### デプロイ自身のエージェント

`OCHAKAI_AGENT` のエージェントが書く draft と、問うた人が適用した修正案は
`process:ochakai via <問うた人>`、producer `ochakai/<版>` として記録される。
人が自分で書いたものと取り違えられず、`created_by=process:ochakai` で全部を
引ける。書き込みは問うた人の範囲で行われ、付与はその人と同じだけ効く。

### 匿名の姿勢

`public` と `sandbox` はヘッダを一つも読まず、全員を `human:anonymous` として
扱う(委譲も producer も読まない)。前に立つ者が誰も検証していないので、
信じればどの名前でも名乗れるからである。`dev` も `human:anonymous` だが、
ローカル開発のために委譲は読む。姿勢そのものは設計記録
[0066](../design/0066-four-postures-one-word.md)・[0087](../design/0087-a-sandbox-says-it-is-one.md) が持つ。

### プロキシは自称を運ばない

`ochakai ui` と `serve-ui` は呼び出し元を上流に**言い直す**部品なので、
ブラウザから来た `Ochakai-On-Behalf-Of` は**常に捨てる**。

- `serve-ui` に `OCHAKAI_IAP_AUDIENCE` があれば、IAP の署名付きアサーションを
  検証し(`iss` も自分で確かめる)、その email から委譲ヘッダを**作り直す**。
  検証できないリクエストは 403。audience は推測しない。
- 無ければ作らず、書き込みは `serve-ui` のサービスアカウント名義になる。
  起動ログがどちらのモードかを言う。
- `ochakai ui` は利用者本人のトークンで代理するので `human:<本人>` になる。

## 認可: ディレクトリの閲覧者と編集者

同じバンドルの中に、見せてはいけない知識と変えられては困る知識がある
利用者のためにある。デプロイを分けると共有語彙が `unverified` のコピーに
なり、検証済みという事実を失う。

### 付与表

`GET|PUT /api/v1/access` の一枚の文書で、**(prefix, principal) → 読む /
書く / 管理する** の付与だけを持つ。

- **prefix はセグメント境界で照合する**(`sales` は `sales/orders` に当たり、
  `sales-legacy/orders` には当たらない)。空の prefix はバンドル全体。
- **principal は台帳と同じ綴り**(`human:…` / `process:…`)と、認証済みの
  全員を指す `*`。委譲された書き込みは利用者として照合し、`via` は見ない。
- **書く は 読む を含み、管理する は 書く を含む。** 拒否規則・role・
  グループ・継承の打ち消しは持たない — 「なぜ読めないか」の答えが、常に
  表の中の一行の有無であるように。
- **行が一つも無ければ、何も変わらない。** 最初の一行が全員に同時に境界を
  入れる。段階的な有効化は無い。

### 管理者

**`OCHAKAI_ADMINS`**(環境変数)が何でもできる principal を並べる。
ポリシーを編集できる者をポリシーの中で決めると回転扉になるので、床は
ochakai を通しては誰も触れない設定に置く。

- ポリシーを持つのに管理者が空のデプロイは**起動しない**。
- **ポリシーの最初の一行を置けるのも管理者だけ**で、それ以外は書く前に
  403 で断る。管理者を一人も名指していないデプロイでの書き込みは 400。
- **`may_admin`** を持つ principal は、その prefix 以下の**規則だけ**を
  読み書きできる。根(空の prefix)には置けず、置こうとすると
  `OCHAKAI_ADMINS` を名指して断る。prefix 管理者を作り、外せるのは完全な
  管理者だけである。
- ポリシーは文書一枚の置き換えである。`If-Match` を送れば、読んだ版が
  まだ立っている間だけ置き換わり、そうでなければ 412 で何も書かない。
  送らなければ後勝ち。`If-None-Match` と `If-Match: *` は 400。
- ポリシーは知識ではないので `/api/v1/bundle` の下に置かず、export にも
  載らない。履歴は持たない。

### 読めないものは、無い

- **範囲外の読みは 404、読めるが書けない先への書き込みは 403**
  (`error.forbidden`)。「そこに何かあるか」への答えが境界そのものだから
  である。強制点は `service.Service` の一箇所で、REST・MCP・Web UI が同じ
  層を通る。
- 検索・一覧・browse・log・`linked_from`・ファイル一覧・browse の件数は、
  フィルタの段階で範囲に絞る(行を後から落とすとページングが壊れる)。
- dry run(`dry_run`)は書き込みが断るところで断る。

**塞がないもの。** 読める concept の本文が隠れた id を名指していれば、
その文字列は読める。保存形は受け取ったバイト列そのもので、redact は往復と
ETag と引き換えになる。約束するのは、一覧・検索・取得・export・index・
log から**出てこない**ことまでである。狭い付与の呼び出し元は `log.md` の
行数上限より少ない行を見ることがある。

### 範囲を持つ呼び出し元の操作

| 操作 | 範囲を持つ呼び出し元 |
|---|---|
| `stats` | 自分の読める範囲(`prefix` との交差)を数え、答えの `scope` に数えた prefix を載せる。空の一覧は「何も見えない」 |
| `move` | 動かす concept・移動先・それを指す全 concept が書ける範囲にあるとき動く。はみ出すなら**丸ごと**断る(移動先が書けなければ 403) |
| export | 読める subtree のアーカイブは取れ、アーカイブが自分の範囲を名乗る。**バンドル全体(根)は管理者だけ** |
| `reembed`、ポリシーの読み書き、他人の turn の読み取り | 管理者だけ |

### 面ごとの現れ方

REST は `/api/v1/access` の二操作、CLI は `ochakai access`(`-f` で置き換え)、
Web UI は `#/access`(ポリシーを読めた呼び出し元にだけタブが出る)。
**MCP には載せない** — 境界の編集は運用者の操作で、ツールスキーマは
エージェントの文脈から払われる。範囲は MCP にも同じ層で効く。

`serve-ui` が IAP 無しで配られ、そのサービスアカウントが
`OCHAKAI_ADMINS` にいれば、その URL に届く全員がポリシーを編集できる。
ページは自分がどちらの経路で配られたかを知らないので、これは文章で
警告する。

## 環境変数

| 変数 | 役割 |
|---|---|
| `OCHAKAI_OIDC_ISSUER` / `OCHAKAI_OIDC_AUDIENCE` | 自分で検証する経路。両方か、どちらも無しか |
| `OCHAKAI_DELEGATING_CALLERS` | 委譲を許す呼び出し元。既定は空 |
| `OCHAKAI_PRODUCER` | CLI が名乗る producer |
| `OCHAKAI_IAP_AUDIENCE` | `serve-ui` が IAP のアサーションを検証する audience |
| `OCHAKAI_ADMINS` | 完全な管理者。付与を置くなら必須 |

## やらないこと

- role・グループのレジストリ・deny・型やタグや concept 単位の付与・
  ポリシーの履歴。グループが要るなら発行者の claim から読む。
- `prefix=` フィルタを認可として使うこと(誰でも任意の値を渡せる)。
- API キー、mTLS、HMAC — どれも secret を増やす。
- User-Agent からの producer の推測、producer ごとの allowlist、版の検査。
- actor の位置に `<producer>/<version>` を置くこと。
- 監査ログ・レート制限。
- テナント列。複数の組織を一人が運用する単位はデプロイかディレクトリで
  ある([0119](../design/0119-an-operated-fleet-is-deployments-or-directories.md))。

## Google Cloud の外

認証だけは OIDC で外でも成り立つ。データベースの資格情報は外では運用者の
仕事に戻り(`OCHAKAI_DATABASE_URL`)、ファイルは GCS が無ければ扱えず、
埋め込みは Vertex AI が無ければ無い。**外を正式に支えるのは、埋め込みが
設定無し・secret 無し・所在地を選べる形で外でも既定になったとき**で、
それまで足場は Google Cloud 一つである(C8 が乗っているのが検索だから)。

## 経緯

設計記録 [0003](../design/0003-gcp-only.md)・[0065](../design/0065-identity-and-provenance.md)・
[0086](../design/0086-a-second-way-to-say-who-is-calling.md)・[0109](../design/0109-a-directory-has-readers-and-writers.md)・
[0115](../design/0115-the-second-footing-waits-for-search.md)・[0117](../design/0117-a-person-recorded-as-a-process-says-so.md)・
[0119](../design/0119-an-operated-fleet-is-deployments-or-directories.md)・[0120](../design/0120-the-policy-is-replaced-only-as-it-was-read.md)・
[0121](../design/0121-each-path-reads-its-own-header.md)・[0122](../design/0122-the-first-rule-is-an-administrators-to-write.md)・
[0123](../design/0123-the-numbers-say-what-they-counted.md)・[0124](../design/0124-a-directory-can-have-its-own-administrator.md)・
[0129](../design/0129-a-move-runs-when-its-rewrite-fits.md)・[0134](../design/0134-an-archive-says-which-part-it-is.md)・
[0151](../design/0151-claude-reaches-the-knowledge-through-the-persons-google-sign-in.md)。
退けた案の全文はそちらにある。
