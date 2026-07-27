# ochakai 設計ドキュメント 0038: stdio しか話せないクライアントに橋を架ける

Status: Accepted(2026-07-27)。[0012](0012-retire-mcp-oauth-connector.md) が
retire した「公開 OAuth コネクタ」を復活させずに同じ需要を満たす。
[0015](0015-surface-consistency.md) の面一貫性の対象外である理由を §4 で
述べる。実装は同 PR
Date: 2026-07-27

## 1. 目的

README は「シェルを持たないエージェント(Claude Desktop など)が MCP の
主対象である」と書いている。しかし **ochakai の MCP は streamable HTTP
だけで、stdio 版が無い。**

- ローカルの `docker compose` に対してなら、HTTP を話せるクライアントは
  `http://localhost:8080/mcp` を指せばよい。ここは問題ない。
- Cloud Run 上の ochakai に対しては、リクエストに Google の ID トークンが
  要る。クライアント自身はそれを取れない。
- そして **stdio しか話せないクライアントは、そもそも URL を指せない。**

つまり README が主対象と呼んだ層に、動く手順が一つも無い。0012 で
公開 OAuth コネクタを retire して以降、この穴は開いたままだった。

`ochakai ui` が部分的な回避策になっている(loopback に立って `/mcp` を
出し、ID トークンを載せる)。だが「MCP を使いたいだけの人に Web UI を
常駐させる」のは道具の説明として苦しく、しかも stdio クライアントには
やはり届かない。

## 2. 決定

**CLI に `ochakai mcp-stdio` を足す。stdio と remote の間を JSON-RPC
メッセージ単位で素通しするブリッジであり、サーバー側には何も足さない。**

```
Claude Desktop ──stdio──> ochakai mcp-stdio ──HTTPS+ID token──> Cloud Run /mcp
```

### 2.1 サーバー面は増やさない

`serve` を stdio で動かす案は採らない。それはクライアントの機械から
データベースに直接つなぐことを意味し、Cloud SQL が private である前提
(0003)と噛み合わない。ochakai の CLI は一貫して REST の薄いクライアント
であり(0004/0007)、このコマンドもその系列に属する。

### 2.2 トランスポート層で素通しする

MCP SDK の `Transport` / `Connection` は `jsonrpc.Message` の Read/Write
であり、両端とも公開されている。したがってブリッジは

- `mcp.StdioTransport{}.Connect()` — stdin/stdout 側
- `mcp.StreamableClientTransport{Endpoint, HTTPClient}.Connect()` — remote 側

の 2 つの `Connection` を双方向にポンプするだけでよい。

**ツールの定義を写さない。** 個々のツールを列挙して再登録する実装
(client を張って `ListTools` し、同名の tool を stdio server に生やす)
も可能だが、それはツール定義を二重に持つことであり、`search_knowledge`
に引数が 1 つ増えるたびにブリッジが古びる。0015 が「面を増やすと同期の
義務が増える」と言っているのは、まさにこの種の複製のことである。
メッセージ素通しなら、`initialize` も `notifications/*` も resource も
将来のメソッドも、ブリッジが知らないまま通る。

セッション ID(`Mcp-Session-Id`)、SSE ストリーム、再接続、Close 時の
DELETE は streamable client transport の内側で閉じている。ブリッジが
HTTP を自前で組み立てないのはそのためである。

### 2.3 identity は既存の経路をそのまま使う

`--url` > `$OCHAKAI_URL` > `ochakai use` の選択、という他の client
コマンドと同じ解決をし、`apiclient.Client` が返す `oauth2.TokenSource`
から ID トークンを取って `HTTPClient` に載せる。**クライアント側に設定
する資格情報は無い**(secret-zero、0002/0003)。ローカルの平文 HTTP
サーバーに対しては token source が nil になり、素の HTTP で送る —
`ochakai ui` と同じ扱いである。

### 2.4 stdout はプロトコル専用である

stdio では **stdout が通信路そのもの**であり、そこに 1 行でも人間向けの
文字列を書いた時点でセッションが壊れる。したがって:

- 診断・エラー・起動メッセージはすべて **stderr** に出す。
- 接続先と identity を起動時に stderr へ 1 行出す。クライアントのログに
  残り、「誰として繋がっているのか」を後から追える。

これは規約ではなく不変条件なので、テストで固定する(§3)。

### 2.5 到達性の確認は起動時に済ませる

他の client コマンドと同じく、先に `Identity()` と `Health()` を呼ぶ。
stdio クライアントは失敗を握り潰しがちで、認証が通っていないことが
「ツールが一覧に出ない」という形でしか現れないと原因に辿り着けない。
起動時に stderr へ落とすほうが早い。

## 3. 検査する不変条件

[0035](0035-verifiability.md) に従い、型に載らないものをテストで外から
留める。

- **stdout に protocol 以外が出ない。** stdout/stderr を差し替えて
  ブリッジを起動し、stdout に現れたバイト列が JSON-RPC メッセージだけで
  あることを確かめる。
- **メッセージが素通しされる。** in-memory の ochakai MCP サーバーを
  立て、`initialize` と `tools/list` を stdin に流し込み、remote が返した
  ツール集合がそのまま stdout に出ることを確かめる。ツール名を列挙して
  期待値に書かない — 素通しの主張は「remote が返したものと一致する」で
  あって「特定の 9 個である」ではない。

## 4. やらないこと

- **REST / Web UI に対応物を作らない。** 0015 が求める面一貫性は*能力*の
  話であり、`mcp-stdio` は新しい能力ではなく既存 MCP 面への**トランスポート
  経路**である。REST に "stdio" は意味を持たない。同じ理由で MCP の
  ツール数(0015 §3.1 の予算)は 1 つも増えない。
- **公開エンドポイントを作らない。** 0012 が retire した OAuth コネクタは
  「インターネットに口を開けたサービス」であり、その到達性そのものが
  retire の理由だった。本ブリッジはユーザーの機械の中のプロセスで、
  listen するポートを持たない — `ochakai ui` が必要としていた DNS
  rebinding / CSRF の防御が、ここでは**そもそも不要**になる。
  0010/0012 の判断は変わらない。
- **stdio 側で認証しない。** このプロセスに話しかけられる者は、すでに
  そのユーザーとして端末でコマンドを実行できる者である。
- **リトライやキャッシュを挟まない。** ブリッジが状態を持てば、それは
  remote と食い違いうる状態である。切れたら落とす。
