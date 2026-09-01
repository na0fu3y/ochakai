# MCP クライアントを繋ぐ

ochakai は `/mcp` で streamable HTTP による MCP を提供する。クライアント
の設定に何を書くかは二つの事実だけが決め、どちらも好みの問題ではない:

**サーバーが identity を読むか。** 読むデプロイに対しては、すべての
リクエストが MCP クライアントの誰も発行できない Google ID トークンを
運ばなければならないので、接続は `ochakai mcp-stdio` を通す —
stdin/stdout で MCP を話し、他のクライアントコマンドと同じ方法で
identity を解決し、JSON-RPC メッセージをそのまま転送する(設計
ドキュメント [0067](../design/0067-four-faces-and-what-they-decline.md) §3)。
クライアント側の設定に資格情報は要らない、そもそも設定するものが無い
からである。**読まないデプロイではこの要件が消え、URL がそのまま
使える** — ローカルの `dev` だけでなく、公開された `public` や
サンドボックスも同じで、これは所在ではなく posture の問題である
(設計ドキュメント [0066](../design/0066-four-postures-one-word.md))。
どちらかは `ochakai whoami` の `posture:` 行が答える。

**クライアントが何を開けるか。** コマンドを起動して stdio で話すこと
しかできないクライアントもある。そうしたクライアントには、サーバーが
どちらであってもブリッジが答えになる。

つまり: **identity を読まないサーバー、かつ対応できるクライアントなら
URL。それ以外はブリッジ** — ブリッジには独自の前提条件があり、どの設定
より前に[下](#what-the-bridge-needs)で挙げる。

| クライアント | identity を読まないサーバー | identity を読むサーバー | 設定の置き場所 |
|---|---|---|---|
| [Claude Code](#claude-code)* | URL | ブリッジ | `.mcp.json`、または `claude mcp add` |
| [Claude Desktop](#claude-desktop) | ブリッジ(または [`.mcpb`](#claude-desktop)) | ブリッジ | `claude_desktop_config.json` |
| [その他のクライアント](#その他のクライアント) | URL | ブリッジ | クライアントごと(下の表) |
| [ホスト型アシスタント](#clients-that-cannot-reach-your-deployment) | 公開デプロイなら届く | 設計上、届かない | 製品ごと |

**何もインストールせずに一番速く繋ぐ**なら、公開デモが URL をそのまま
取る — Claude Code なら一コマンドで、Go も gcloud も要らない:

```sh
claude mcp add --transport http ochakai https://demo.ochak.ai/mcp
```

自分のサーバーに対する URL は、ローカルの `docker compose` なら
`http://localhost:8080/mcp` で、`deploy/compose.yaml` が渡すものその
ものである。

\* Claude Code はこの表の唯一の例外である: シェルを持っているので、
[推奨される経路は CLI](#claude-code) であって MCP ではない。

**設定を全文で書き下すのは二つだけである** — Claude Code とブリッジ、
つまりこのプロジェクトが実際に動かしているものである。残りは形が同じで
JSON のキー名が違うだけなので、[その他のクライアント](#その他のクライアント)
がその一行ずつを持つ。

## エージェントが手にするもの

| ツール | 説明 |
|---|---|
| `search_concepts` | 型をまたいだ検索。verified な concept が上位に来る。順位なので `limit` で終わり、二ページ目は無い。データの質問はここから始める |
| `list_concepts` | レビューのフィードを端まで歩く(`verified_at` / `usage` / `failed` / `stale_after`)。`cursor` で続きを引く。検索ではないので `query` は取らない(設計ドキュメント [0096](../design/0096-a-listing-is-not-a-search-here-either.md)) |
| `get_concept` | concept を一件、OKF ドキュメントとして取得する。ファイルのメタデータと、この concept を本文から指す concept の行(`linked_from` — metric の読み方を言う insight はここに出る)が付く |
| `get_file` | concept に添付されたファイルを取得する(ダッシュボードのスクリーンショット、ER 図、seeds ファイルなど) |
| `put_concept` | 学びを書き戻す — id が空いていれば作成し、埋まっていれば置き換える。変更はすべてリビジョンとして残る |
| `report_outcome` | ナレッジをもとに行動した後、worked/failed を報告する — failed の報告は verified な concept を再検証フィードに乗せる。`note` に何が起きたかを書けば、フィードを読む人がそれを読む(設計ドキュメント [0137](../design/0137-a-report-says-what-it-saw.md))。**返ってくるのは合計だけで、note の一覧は返らない** — 読むのは REST・CLI・Web UI の側である |

これらはすべて知識に関する操作である。ochakai は SQL を実行せず、LLM も
呼ばない。`compile_sql` — セマンティックモデルからの決定的な SQL 生成 —
は 0.13.0 まで存在し、その後退役した(設計ドキュメント
[0070](../design/0070-what-was-retired-and-why.md) §3): エージェントが実際に
必要としているのは検証済みのクエリとそれに添う注意書きであり、検索が
前者を見つけ、取った concept の `linked_from` が後者を名指す(設計
ドキュメント [0106](../design/0106-a-read-carries-what-points-at-it.md))。

**削除と利用回数の合計はこの表に無い。** `delete_concept` と
`get_concept_usage` は設計ドキュメント
[0076](../design/0076-two-tools-leave-mcp.md) で降ろした — 削除は裁定で
あり、利用回数はループの人間側だからである。どちらも REST(`DELETE
/api/v1/bundle/{path}`・`GET /api/v1/usage/{id}`)、CLI(`ochakai delete`・
`ochakai usage`)、Web UI には残っている。

すべての concept は正規の URI で参照できる **MCP リソース**でもある —
`ochakai://` の後ろに id(concept のパス)を続ける、例えば
`ochakai://metrics/revenue` や `ochakai://queries/sales/top-customers`。
リソース参照(`@` メンション)に対応するクライアントは、ツール呼び出し
無しで concept を OKF ドキュメント — frontmatter と本文 — として引き
込める。発見のための手段は引き続き `search_concepts` である。

読み取り系のツールには `readOnly` の、書き込み系には
`destructive: false` のアノテーションが付いているので、クライアントの
自動承認ポリシーは説明文を解析しなくても機能する。破壊的なツールは
一本も無い。

ツール数は REST の写しではなく予算である: スキーマはエージェントの
コンテキストウィンドウから支払われるので、REST API はその上位互換に
なる — 一括エクスポート、人が読むための取得、削除、利用回数の合計、
そしてファイルの書き込みは無い(設計ドキュメント
[0067 §5](../design/0067-four-faces-and-what-they-decline.md))。シェルを
持つエージェントには、[CLI](../cli.md) が全機能をカバーしつつスキーマ
の代金を一切要求しない。

<a id="what-the-bridge-needs"></a>

## ブリッジが必要とするもの

下の設定のうちコマンドを起動するものはどれも、クライアントの `PATH`
上に `ochakai` を必要とする。identity を読むデプロイに対してはさらに
二つ必要で、どちらもマシンに既定では入っていない:

- **`gcloud` CLI。** ID トークンの出どころである。ブリッジはまず
  サービスアカウントの ADC を試し、それが無ければ
  `gcloud auth print-identity-token` にフォールバックする —
  ユーザー自身の application-default credentials では Cloud Run が
  求める audience 束縛のトークンを発行できないので、個人のマシンでは
  `gcloud` の経路が実際に動く。
- **`gcloud auth login` を済ませていること。** サービスに対する
  `roles/run.invoker` を持つ principal としてである。

CLI は Cloud Run の URL を直接指したとき(`ochakai use
https://your-service.run.app`)にも同じ方法で同じトークンを解決する —
この一覧は実質、ブリッジであるかどうかによらず Cloud Run に届くために
必要なものである。

「クライアントは資格情報無しで設定される」というのは設定ファイルに
ついての主張であって、マシンについての主張ではない。資格情報自体は
存在する。ただそれが JSON の中ではなく gcloud のセッションに住んで
いるだけであり、それがクライアントの設定からも、ochakai がローテー
ションしなければならないどんな形のディスク上からも、資格情報を締め
出している。

`ochakai whoami` が確認の手段である。CLI がどのサーバーに、誰として
話しかけていて、それが応答するかを表示するので、下の設定を編集する
前に端末で走らせておく — identity を持たずにブリッジを起動した
クライアントは、たいてい「サーバーにツールが無い」という形でそれを
報告するからである。

### 切れるのはトークンではなくセッションである

ID トークンは一時間で失効するが、**それは誰も触らずに回る**: ブリッジ
はトークンの `exp` を読んで期限を知り、切れていれば次のリクエストの
ために発行し直す。設定を書いたあと繰り返し打つコマンドは無い。

切れるのはその下の **gcloud のセッション**のほうで、いつ切れるかを
決めるのは ochakai ではなくあなたの組織である — Google Workspace の
セッション長ポリシー、パスワードの変更、長い無活動、`gcloud auth
revoke`。復旧は端末で `gcloud auth login` を打ち直すことだけで、
**動いているクライアントを再起動する必要は無い**: 次の呼び出しが新しい
セッションからトークンを取る。例外は起動時で、ブリッジは identity を
解決できないと即座に終了するため(それを「ツールが無い」として黙らせ
ないためである)、その場合だけクライアント側の再接続が要る。

これより永続的な形はサービスアカウントの ADC だが、それは鍵をディスク
に置くことを意味する。ochakai が secret を一つも持たない姿勢
([0065](../design/0065-identity-and-provenance.md))で到達できる永続性の
上限が、この gcloud セッションである。

以上は identity を読むデプロイに対する話で、トークンを一切要求しない
`dev` や `public` のサーバーには当てはまらない。

## Claude Code

**推奨は CLI、MCP ではない。** Claude Code はシェルを持つので、CLI の
ツールスキーマはエージェントのコンテキストを消費しない —
`--help` はその場で読める — し、Cloud Run に対してもプロキシやブリッジ
プロセスを要らない。ID トークンを自分で解決するからである。README の
[エージェントを繋ぐ](../../README.md#connect-an-agent)を見よ。

### それでも MCP のツールが欲しい場合

identity を読まないサーバー(ローカルの `docker compose`、公開デモ)に
対して:

```sh
claude mcp add --transport http ochakai http://localhost:8080/mcp
```

identity を読むサーバーに対して、ブリッジ経由:

```sh
claude mcp add ochakai -- ochakai mcp-stdio
```

`-s user` はプロジェクトではなくユーザーの設定に置く。既定のスコープは
プロジェクトディレクトリに閉じたローカルである。`-s project` は
`.mcp.json` に書き込む — これはコミットされる形で、このリポジトリ自身
の `.mcp.json` はローカルの `docker compose` サーバーを指し、それが
動いていれば自動的に繋がる。

その `.mcp.json` は `Ochakai-On-Behalf-Of` ヘッダも載せている:

```json
{
  "mcpServers": {
    "ochakai": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": { "Ochakai-On-Behalf-Of": "process:claude-code" }
    }
  }
}
```

**`dev` のサーバーに書き戻すつもりなら、これを省いてはならない。**
`dev` は呼び出し元を全員 `human:anonymous` として記録するので、ヘッダの
無い接続から書かれた concept は人が書いたものと区別が付かなくなる —
エージェントの下書きを人の裁定と取り違えないための区別が、台帳から
消える。ヘッダを付ければ `process:claude-code via human:anonymous` と
両方が残る。`dev` では誰でも委譲できるので
`OCHAKAI_DELEGATING_CALLERS` を設定する必要は無く、本番ではそれが
呼び出し元を許していることが要る([設定](../configuration.md#environment-variables))。
identity を読むデプロイでは呼び出し元が本物の Google の身元を運ぶので、
自分を名乗らせるためにこのヘッダを足す理由は無い。

## Claude Desktop

**バンドルが一番早い。** リリースページの `ochakai_<version>.mcpb` を
開くと、アプリがそれをインストールし、訊いてくるのは**サーバーの URL
一つだけ**である(公開デモが既定で入っている)。中身は下の JSON と
同じブリッジで、書くのがあなたではなくなるだけである。macOS と Windows
向けで、バンドルを入れられるのがその二つだからである。

バンドルは gcloud を不要にはしない。Cloud Run に対しては下の
[ブリッジが必要とするもの](#what-the-bridge-needs)がそのまま要る —
バンドルが持っているのは ochakai のバイナリであって、あなたの
Google の身元ではない。公開デモと、IAM の後ろに居ないデプロイは、
それだけで動く。

自分で書く場合。デスクトップアプリの設定ファイルは URL ではなく
コマンドを取るので、どちらの場合もブリッジが経路になる:

```json
{
  "mcpServers": {
    "ochakai": { "command": "ochakai", "args": ["mcp-stdio"] }
  }
}
```

ファイルは macOS では
`~/Library/Application Support/Claude/claude_desktop_config.json`、
Windows では `%APPDATA%\Claude\claude_desktop_config.json` にある —
設定 → Developer → Edit Config が開いてくれる。編集後はアプリを
再起動する。

`command` には絶対パスを使う(`which ochakai`)。デスクトップアプリは
シェルの `PATH` を継承しないので、正しく見える設定がツールを一つも
出さないのはたいていこれが理由である。

Cloud Run に対しては、この設定だけではマシンに gcloud とログインが
揃うまで何も起きない —
[ブリッジが必要とするもの](#what-the-bridge-needs)。アプリの中から
それを与える方法は無く、それを不要にするバンドルやインストーラも無い。

`ochakai use` が選んだのと違うサーバーを使うときは、下のように
`--url` を足す:

```json
{
  "mcpServers": {
    "ochakai": {
      "command": "/usr/local/bin/ochakai",
      "args": ["mcp-stdio", "--url", "https://your-service.run.app"]
    }
  }
}
```

アプリの**カスタムコネクタ** UI はコマンドではなく URL を取るが、
その接続はあなたのマシンではなく Anthropic のインフラから開かれる —
[下](#clients-that-cannot-reach-your-deployment)を見よ。

## その他のクライアント

**二つの形はどのクライアントでも同じで、違うのは JSON のキー名だけ
である。** URL 形式は上の `http://localhost:8080/mcp` を、ブリッジ形式は
[Claude Desktop](#claude-desktop) の `command` / `args` を、下の器に
そのまま入れる。

| クライアント | 設定の置き場所 | URL 形式の器 |
|---|---|---|
| Cursor | `~/.cursor/mcp.json`(全プロジェクト)、`.cursor/mcp.json`(一つ) | `mcpServers` → `url` |
| VS Code | `.vscode/mcp.json`、または **MCP: Open User Configuration** | `servers` → `type: "http"` と `url` |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` | `mcpServers` → **`serverUrl`** |
| Cline | **MCP Servers → Configure MCP Servers**(ドキュメントとソースがパスで食い違うので、パネルから開く) | `mcpServers` → `type: "streamableHttp"` と `url` |
| Zed | `~/.config/zed/settings.json` | `context_servers` → `url` |
| Gemini CLI | `~/.gemini/settings.json`、またはプロジェクトごとの `.gemini/settings.json` | `mcpServers` → **`httpUrl`** |

ブリッジ形式はどれも `url` の項目を `command` / `args` に置き換えたもの
で、VS Code だけ `"type": "stdio"` を添える。

**キー名を間違えると黙って失敗する。** Gemini CLI の `url` は SSE を
意味するので `/mcp` に向けると原因の分からない形で落ち、Cline は `type`
の綴りを間違えると黙って SSE にフォールバックする。この二つは
[繋がらないとき](#繋がらないとき)より先に疑う。

この六つは各クライアント自身のドキュメントから 2026 年 7 月時点で
写したもので、**ここでは誰も動かしていない** — 正はそれぞれのクライアント
のドキュメントであり、食い違っていたら issue にする価値がある。設定の
全文を置かないのはそのためである: 動かしていない設定が古びたことには、
誰も気づかない(設計ドキュメント
[0068](../design/0068-how-a-face-is-added-and-removed.md) §3)。

## identity を読むサーバーに対して機能する URL

クライアントが URL しか取らず、サーバーが identity を読むなら、
`ochakai ui` がその URL を与えるローカルのプロキシである。あなたの identity
で認証し、web UI と並べて `/mcp` を、ループバックに束縛して公開する:

```sh
ochakai ui        # http://127.0.0.1:8098/mcp
```

`gcloud run services proxy` が三つ目の方法である — MCP 固有の振る舞い
を持たない、ただの HTTP トンネル。deploy ガイドはこれを `curl` 越しの
デプロイ検証に使っているが
([§3](../../deploy/cloudrun/README.md#3-deploy-cloud-run-dedicated-identity-passwordless-org-restricted))、
クライアントを繋ぐためではない。そちらにはブリッジか上の
`ochakai ui` を使う。

<a id="clients-that-cannot-reach-your-deployment"></a>

## デプロイに届かないクライアント

ChatGPT のコネクタ、OpenAI の Responses API、Claude Desktop のカスタム
コネクタ UI は、いずれも接続を**あなたのマシンからではなくベンダーの
インフラから**開く。`localhost` も IAM で制限された Cloud Run サービス
も、そこからは届かず、どんな設定の書き方もそれを変えない。

これはギャップではなく、アクセスモデルが働いている証拠である:
到達できることそのものが認可である(設計ドキュメント
[0065](../design/0065-identity-and-provenance.md) §1)ので、デプロイを第三者のサーバー
から届くようにすることは、そのまま公開到達可能にすることを意味する。
チームの知識を持つデプロイをそこへ置くのは、デプロイの一形態ではなく
設定ミスである。ローカルコードも動かせるホスト型アシスタント
(ブリッジ越しの Claude Desktop、Claude Code、Cursor)が、サポートされる
経路である。

**例外は、公開することが決定であるデプロイだけである** — `public` と
サンドボックス([0066](../design/0066-four-postures-one-word.md) §3・
[0087](../design/0087-a-sandbox-says-it-is-one.md))は、誰から届くかを
選んでいないのだから、ベンダーのインフラから届いても失うものが無い。
公開デモがその形で、ここでは誰も動かしていないので、各アシスタントの
コネクタ UI がそれを受け入れるかどうかは、それぞれの製品の話である。

## 繋がらないとき

- **ツールが一つも出ず、エラーも出ない。** クライアントはブリッジを
  起動したが、ブリッジがサーバーに届かなかったか、`ochakai` が
  クライアントの `PATH` に無かった。端末で `ochakai whoami` を走らせる
  — どのサーバーに誰として話していて、応答するかを表示する。その上で
  `command` に絶対パスを使う。
- **ツールは出るが、呼び出しがすべて失敗する。** たいてい identity の
  問題である: Cloud Run に対してはブリッジが `gcloud auth login`
  (または ADC)を済ませている必要があり、呼び出し元は
  `roles/run.invoker` を持っている必要がある。deploy ガイドの
  [§7](../../deploy/cloudrun/README.md#7-troubleshooting-in-a-security-hardened-org)
  が Google 側の症状を扱う。
- **サーバーは curl には応答するがクライアントには応答しない。** パス
  を確認する — `/mcp` であって、ルートではない。ochakai サーバーへの
  `GET /` は、それが提供するエンドポイントを表示する。
- **concept があるはずのベースで検索が空を返す。** 接続の問題ではない。
  日本語のナレッジベースは埋め込みを有効にしておきたい —
  [architecture の検索の節](../architecture.md#search)を見よ。

`ochakai mcp-stdio` は診断情報を stderr に書き、stdout はプロトコル
専用に残すので、サーバーのログを表示するクライアントは、それが見た
ものをそのまま表示する。
