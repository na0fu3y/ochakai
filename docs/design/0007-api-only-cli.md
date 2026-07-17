# ochakai 設計ドキュメント 0007: DB 直結コマンドの廃止(API 一本化)

Status: Accepted(2026-07-17 の議論で決定)
Date: 2026-07-17

## 1. 目的

CLI から **DB 直結の server admin コマンドを廃止し、データの出し入れをすべて
API 経由に一本化する**。0004 決定 3 は `serve` / `import-ossie` / `export-okf`
(のちに `import-okf` も追加)を DB 直結のまま残したが、本ドキュメントは
これを覆す。残る DB 直結コマンドは `serve` のみとなる。

## 2. 動機

### 2.1 二重実装と機能乖離

`export-okf` / `import-okf` は client の `export` / `import` とほぼ同じ処理の
二重実装だった。しかも `--dry-run` / `--keep-root` は client 側にしかなく、
既に機能が乖離し始めていた。同じ成果物に 2 つの経路があると、以後の仕様追加の
たびに片方だけ直る事故が起きる。

### 2.2 利用者の混乱(先行事例)

類似のシングルバイナリツールを調査した結果:

- **etcd** は API 経由(etcdctl)とディスク直接操作(etcdutl)の同居が混乱を
  招いたとして v3.5 でバイナリを分割し、v3.6 で重複機能を削除した。
- **Gitea** の `gitea dump`、**Grafana** の `grafana cli` は DB 直結のまま
  同居する設計で、設定ファイルの解決・実行ユーザー・コンテナ内実行をめぐる
  混乱が慢性的に報告されている。
- **Vault / Consul** はスナップショット取得のような管理操作すら稼働中サーバー
  への認証付き API 呼び出しで行い、ストレージ直結コマンドをほぼ持たない。

ochakai の従来構成は Gitea 型(ヘルプの区分けだけで同居)であり、
最も混乱報告が多いパターンだった。目指すべきは Vault 型である。

### 2.3 provenance の一貫性

DB 直結コマンドは actor を OS ユーザー名から合成していた(`human:<username>`)。
API 経由なら Cloud Run が検証した呼び出し元識別(0002)がそのまま記録され、
provenance の信頼性が上がる。

## 3. 決定

1. **`export-okf` / `import-okf` を削除する。** 完全に client の
   `export` / `import` の劣化版であり、代替はそのまま存在する。
   旧コマンド名は「削除済み。`ochakai serve` を DB の隣で起動して
   `ochakai export` / `import` を使う」と案内するスタブとして残す。
2. **`import-ossie` は client コマンドに移行する。** 代替 API が無かったため
   `POST /api/v1/import/ossie` を新設し(ボディは Ossie YAML そのまま、
   応答は models / created / updated のレポート)、CLI は同名のまま
   API 経由になる。利用者から見える変化は「DB の隣で実行」が
   「サーバーに向けて実行」になることだけ。
3. **DB の隣でしか動かないコマンドは `serve` だけになる。** ヘルプの
   「Server admin commands」区分は「Server command」となる。
4. サーバー無しで DB に流し込みたい場面(初期シード、リストア)は、
   `OCHAKAI_INSECURE_DEV=true ochakai serve` をローカルに起動して client
   コマンドを向けることで満たす。専用コマンドは作らない。

## 4. 影響

- Cloud Run 運用ガイドから Cloud SQL Auth Proxy / authorized network の
  手順が消え、`import-ossie` は `$OCHAKAI_URL` に向けた 1 コマンドになる。
- compose クイックスタートの `docker compose exec` と examples の
  ボリュームマウントが不要になり、curl だけで完結する。
- 0004 決定 3 のうち「`import-ossie` / `export-okf` を DB 直結で残す」は
  本ドキュメントで廃止。`serve` の扱いと help の二区分表示は維持する。
