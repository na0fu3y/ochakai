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

全 17 領域が書かれている。[設計記録の index](../design/README.md) は
履歴の目録になり、以後は書き換えない。

| 文書 | 領域 | 状態 |
|---|---|---|
| [architecture.md](architecture.md) | 全体アーキテクチャとデータエージェント | 現行 |
| [identity.md](identity.md) | 認証・identity・認可(secret-zero と OIDC を含む) | 現行 |
| [deployment.md](deployment.md) | デプロイの姿勢と環境変数 | 現行 |
| [okf.md](okf.md) | OKF 互換・バンドル・保存形・往復と provenance | 現行 |
| [addressing.md](addressing.md) | 住所・パス・move | 現行 |
| [vocabulary.md](vocabulary.md) | 型の語彙と呼び名 | 現行 |
| [files.md](files.md) | ファイル | 現行 |
| [search.md](search.md) | 検索と埋め込み | 現行 |
| [faces.md](faces.md) | 面の配分(REST / MCP / CLI / Web UI) | 現行 |
| [webui.md](webui.md) | Web UI | 現行 |
| [loop.md](loop.md) | 検証ループと利用測定 | 現行 |
| [concurrency.md](concurrency.md) | 同時実行と削除 | 現行 |
| [seeding.md](seeding.md) | 空のベースを埋める | 現行 |
| [rest-stability.md](rest-stability.md) | REST の安定性契約 | 現行 |
| [mcp-cli-stability.md](mcp-cli-stability.md) | MCP・CLI の安定性契約 | 現行 |
| [declined.md](declined.md) | やらないと決めたこと(断り方と撤去の基準。一覧の正は ROADMAP) | 現行 |
| [quality.md](quality.md) | 実装の品質ゲート | 現行 |

決定の書き方そのものは、この一覧ではなく CONTRIBUTING.md が持つ。
[docs/architecture.md](../architecture.md) は利用者向けの要約として残り、
`architecture.md` が書かれたらそちらを引く形に畳む。
