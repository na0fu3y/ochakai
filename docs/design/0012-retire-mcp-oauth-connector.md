# ochakai 設計ドキュメント 0012: MCP OAuth コネクタサービスの撤去

Status: Accepted(2026-07-18)
Date: 2026-07-18

## 1. 決定

[0010](0010-mcp-oauth-connector.md) で導入した MCP OAuth コネクタサービス
(claude.ai / ChatGPT リモートコネクタ向けの公開第二サービス)を**撤去**する。
0010 を supersede し、[0002](0002-authn-authz.md) §4 の元の整理 —
「MCP OAuth は作らない、必要になったら」— に戻す。

削除するもの:

- `internal/connector` 一式(OAuth AS、CIMD 検証、Google 委譲、レート制限)
- `internal/store/oauth.go` と `oauth_*` テーブル(マイグレーション 0008 で
  drop。作成側の 0006 はファイルごと削除 — 新規 DB には最初から作られない)
- `Config.Connector` / `OCHAKAI_CONNECTOR_*` の設定処理
- deploy ガイド §5c、SECURITY.md のコネクタ面

## 2. 理由

0010 の技術判断(CIMD で最小 AS が小さく作れる、代替案が不成立)は
覆っていない。覆ったのは**費用対効果の前提**:

- **ユースケースが具体化しなかった。** ローカルからのアクセスは
  `ochakai` CLI(gcloud ID トークン自己解決)または
  `gcloud run services proxy` で足りており、ブラウザからのアクセスは
  webui サンプル + IAP で提供できる(deploy ガイド §5b)。
  claude.ai / ChatGPT の Web コネクタ経路を今必要とする利用者がいない。
- **リスク総量に対して面が大きい。** コネクタは (1) デプロイ全体で唯一の
  秘密(Google OAuth client secret)、(2) 唯一の `allUsers` サービス、
  (3) 唯一の Domain Restricted Sharing 例外、(4) SSRF・token 総当たり・
  redirect 偽装といった公開 AS 固有の攻撃面、をすべて一人で持ち込む。
  撤去すると **デプロイ全体が secret-zero・全サービス非公開・
  組織ポリシー例外なし**に戻る。
- **保守面積。** 実装 + テストで ~1,700 行が、利用ゼロのまま
  セキュリティレビューの主対象であり続けていた
  (SECURITY.md の「とくに歓迎する報告」の過半がコネクタ)。

## 3. 撤去の安全策

コネクタとして稼働中のデプロイは `--allow-unauthenticated` で公開されている。
撤去後のバイナリが `OCHAKAI_CONNECTOR_PUBLIC_URL` を(0.8.0 以降の他の
廃止変数と同様に)黙って無視すると、**公開サービスのまま本体モード —
ヘッダ自称を信頼する httpauth — で起動してしまう**。これは即座に
なりすまし可能な構成なので、この変数だけは例外的に**起動拒否ガード**とする:
設定されていれば起動せず、コネクタサービスの削除(サービスごと)を促す。

正しい撤去手順はアップグレードではなく削除:

```sh
gcloud run services delete ochakai-connector --region=$REGION
# 併せて: Google OAuth クライアントと Secret Manager の client secret、
# Domain Restricted Sharing の例外(タグ / 条件付きポリシー)も片付ける
```

本体サービス(非公開・IAM)は無変更・無影響。`oauth_*` テーブルの drop は
本体が一切参照しないテーブルの削除であり、ロールバックしても本体の動作には
関係しない。

## 4. 再実装の条件

0010 は設計として有効なまま残す(Status は Superseded)。以下のいずれかが
具体化したら、0010 を出発点に再実装する:

- claude.ai / ChatGPT の Web コネクタから組織ナレッジを引きたい利用者の出現
- proxy なし直結が必須の MCP クライアント要件

その際も 0002 の原則(到達 = 読み書き、認可なし、OAuth は到達制御 +
provenance のみ)は維持する。
