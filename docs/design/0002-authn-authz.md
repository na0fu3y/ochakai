# ochakai 設計ドキュメント 0002: 認証・認可

Status: Accepted(2026-07-15)
Date: 2026-07-15

## 1. 原則

**到達できた者は読み書きできる。** 誰を到達させるかはデプロイ側(Cloud Run IAM /
プライベートネットワーク)の責務で、**ochakai は認可制御を一切持たない**。

ochakai がヘッダから読むのは **provenance(誰として記録するか)だけ**。
`status=verified` への遷移も制限しない(0001 の決定 2 を撤廃)— 誰が検証したかは
`verified_by`(human/agent + 名前)として常に記録されるので、信頼の判断は
参照側が provenance を見て行う。認可ではなく記録で担保する。

role・スコープ・トークン発行/失効といった認可機構は**作らない**。
「読めるが書けない人」が必要になったらこのドキュメントを改訂する。

## 2. 認証モード

`OCHAKAI_AUTH` で選ぶ(排他):

### `cloudrun-iam`(Cloud Run での推奨・トークンレス)

Cloud Run の IAM(invoker チェック)を通過したリクエストには、Google 検証済みの
呼び出し元 ID トークンがヘッダで届く:

- `X-Serverless-Authorization` — 検証後、署名を `SIGNATURE_REMOVED_BY_GOOGLE` に置換して転送。
  両ヘッダがある場合 Cloud Run はこちらのみ検証するため、**アプリもこちらを優先して読む**
- `Authorization` — そのまま転送

クレームから actor を解決する: 名前 = `email`、kind = `*.gserviceaccount.com` なら
agent、それ以外は human。署名の再検証はしない(IAM 通過 = 検証済み)。

- **絶対条件: サービスが非公開(IAM enforced)であること。** 公開サービスでは
  ヘッダは自称にすぎない。README とデプロイガイドに明記する。
- 利用体験: `claude mcp add --transport http ochakai http://localhost:8787/mcp`。
  ヘッダ設定不要、トークン運用ゼロ、actor は自動で本人のメール。
- 人間の資格情報で動くエージェントは human として記録される(git commit と同じ
  「あなたの鍵で動くものはあなた」モデル)。agent として区別したい場合は
  エージェント専用 SA で接続する。

### `clients`(非 GCP / ローカル向け・現行のまま)

`OCHAKAI_CLIENTS` の静的トークン(現 v0.1.x の実装)。compose 等 Cloud Run 以外の
環境用フォールバック。DB 管理化・失効 API などは**作らない**(必要になるまで)。

### 未設定(開発用)

現行どおり human:anonymous。

## 3. 経路ごとの整理

| 経路 | 構成 | actor |
|---|---|---|
| Claude Code / MCP | gcloud proxy → cloudrun-iam | human:本人メール |
| ヘッドレスエージェント | 専用 SA → cloudrun-iam | agent:SA メール |
| sample webui | webui の SA が IAM 通過 | agent:webui SA(全利用者が集約される。per-user が必要になったら IAP 検証を将来検討) |
| 非 GCP | clients モード | トークンのマップ通り |

## 4. 将来の選択肢(作らない、必要になったら)

- MCP OAuth(Issue #5): 公開エンドポイントで proxy なし直結が必要になったら
- IAP JWT 検証: webui 利用者の per-user provenance が必要になったら
- 認可(role / スコープ): 「読めるが書けない人」を作る必要が生まれたら

## 5. v0.2 実装スコープ

1. `OCHAKAI_AUTH=cloudrun-iam` の追加(ID トークンのクレーム解析 → actor 解決、
   X-Serverless-Authorization 優先)
2. `OCHAKAI_VERIFY_ACTORS` と verified 昇格制限の撤廃(0001 決定 2 の supersede)

既存デプロイ(clients モード)は無変更で動作。verified を agent が付けられるように
なる点のみ挙動変更。
