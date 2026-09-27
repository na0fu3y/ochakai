# ochakai の現行仕様

ochakai が**いまどう動くか、なぜそうなのか**を、領域ごとに一本ずつ書く
場所である。ここの文書は**書き換える**。振る舞いを変える PR は、同じ PR で
その領域の文書を直す。改訂の履歴は git が持つので、「改訂」「Superseded」
「Status の注記」はここには無い。いつも現在形で書く。

問い直されそうな選択 — 退けた案と戻す条件を残す価値のあるもの — は、
[決定ログ](../decisions/README.md)に短く一本足し、ここの文書から引く。
書き方の規則は [CONTRIBUTING.md](../../CONTRIBUTING.md) の
「Design: spec and decisions」にある。

## 領域

**まだ書かれていない領域は、[設計記録の index](../design/README.md) の
早見表の行が現行の読み先である。** 一本書き終えるたびに、下の行と
早見表の行がここを指すようになる。全部が揃った時点で
`docs/design` は履歴の目録になり、以後は追記しない。

| 文書 | 領域 | 状態 |
|---|---|---|
| [architecture.md](architecture.md) | 全体アーキテクチャとデータエージェント | 現行 |
| [identity.md](identity.md) | 認証・identity・認可(secret-zero と OIDC を含む) | 現行 |
| `deployment.md` | デプロイの姿勢と環境変数 | 未着手 |
| `okf.md` | OKF 互換・バンドル・保存形・往復と provenance | 未着手 |
| `addressing.md` | 住所・パス・move | 未着手 |
| `vocabulary.md` | 型の語彙と呼び名 | 未着手 |
| `files.md` | ファイル | 未着手 |
| [search.md](search.md) | 検索と埋め込み | 現行 |
| `faces.md` | 面の配分(REST / MCP / CLI / Web UI) | 未着手 |
| `webui.md` | Web UI | 未着手 |
| `loop.md` | 検証ループと利用測定 | 未着手 |
| `concurrency.md` | 同時実行と削除 | 未着手 |
| `seeding.md` | 空のベースを埋める | 未着手 |
| `rest-stability.md` | REST の安定性契約 | 未着手 |
| `mcp-cli-stability.md` | MCP・CLI の安定性契約 | 未着手 |
| `declined.md` | やらないと決めたこと(ROADMAP の「やらないこと」と一本化) | 未着手 |
| `quality.md` | 実装の品質ゲート | 未着手 |

決定の書き方そのものは、この一覧ではなく CONTRIBUTING.md が持つ。
[docs/architecture.md](../architecture.md) は利用者向けの要約として残り、
`architecture.md` が書かれたらそちらを引く形に畳む。
