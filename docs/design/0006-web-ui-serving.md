# ochakai 設計ドキュメント 0006: Web UI の二つの配信経路

Status: Accepted(2026-07-17)
Date: 2026-07-17

## 1. 目的

バンドルされた Web UI(自己完結の `index.html` 1 ファイル、ビルド不要・
フレームワークなし・CDN なし)を、認証モデルの異なる二つの経路で配信する。

UI 自体は認証を知らない — 自オリジンの相対パス(`/api/v1`)を叩くだけで、
資格情報の付与は手前に立つリバースプロキシの仕事。この分離により同じ
ページが両経路で共有できる。ページの実体は `internal/webui/index.html`
(`go:embed`)で、`ochakai ui` と examples/webui の両方が import する。

| | `ochakai ui`(CLI 組み込み) | examples/webui(Cloud Run) |
|---|---|---|
| 想定用途 | 個人利用。デプロイ不要 | チーム共有の常設 URL |
| トークンの出所 | 利用者の ADC / gcloud(0004 §4 と同一) | サービスのメタデータサーバー |
| ヘッダ | `Authorization` | `X-Serverless-Authorization` |
| 記録される識別 | `human:<本人の email>` | `agent:<webui の SA>` |
| 待受 | 127.0.0.1 のみ | 公開(必要なら IAP) |

## 2. 先行例

サーバー同梱型(Consul/Vault/Gitea 等、単一バイナリのサーバーが UI も配信)は
採らない。Cloud Run IAM の背後ではブラウザが ID トークンを付けられず、
0002/0003 の認証モデルと衝突するうえ、サーバーの提供面を最小に保つ方針
(deploy ガイド §5b)にも反する。採るのはクライアント側ローカル UI 型 —
`kubectl proxy` + Dashboard、`argocd admin dashboard`、`go tool pprof -http`
と同型で、「CLI が資格情報を貸してローカルに UI を立てる」。

## 3. `ochakai ui` の安全上の設計

利用者のトークンで書き込めるプロキシなので:

- **127.0.0.1 固定バインド**(`--port` のみ可変)。LAN 共有の要件は
  Cloud Run 経路が担う。
- **Host ヘッダ検証**: loopback 名(`localhost` / `127.0.0.1` / `::1`)
  以外は 403。DNS リバインディング(攻撃ページが自ドメインを 127.0.0.1 に
  解決させ same-origin でプロキシを叩く)への防御。kubectl proxy の前例。
- **ブラウザ由来の `Authorization` は転送前に必ず破棄**し、CLI が解決した
  トークンで置き換える。

`/mcp` もプロキシするため、`gcloud run services proxy` なしで
`claude mcp add --transport http ochakai http://127.0.0.1:8098/mcp` が
本人の識別で動く副効果がある。

## 4. 非目標

- UI のための新しい認証機構(OAuth 等)は導入しない。ブラウザの per-user
  識別を公開 URL で得る話は Issue #5(MCP OAuth)/ IAP の領域のまま。
- UI をサーバーバイナリ(`ochakai serve`)から配信することはしない。
